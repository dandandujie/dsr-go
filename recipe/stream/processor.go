package stream

import "github.com/dandandujie/dsr-go/core/jsonx"

// Processor parses backend inference chunks and produces protocol response
// events.
type Processor struct {
	generator Generator
	options   ParsingOptions
	machine   *StateMachine
	stashed   *stashedChunks
	decoder   *StreamDecoder

	started         bool
	promptUsage     PromptUsage
	completionUsage CompletionUsage
	backendFinish   *InferenceFinishReason
	stopSequence    *string
	done            bool
}

// NewProcessor combines a protocol event generator with output parsing
// options.
func NewProcessor(generator Generator, options ParsingOptions) *Processor {
	return &Processor{
		generator: generator,
		options:   options,
		machine:   NewStateMachine(options),
		stashed:   newStashedChunks(),
	}
}

// WithTokenizer attaches the decoder used for token-id chunks.
//
// IDs that contribute text count as completion tokens, including the IDs
// buffered while a multi-token character was incomplete. IDs still buffered at
// the end of input contribute neither text nor completion usage. Without a
// decoder, a token chunk fails the stream with ErrMissingTokenizer.
func (p *Processor) WithTokenizer(decoder TokenizerDecoder) *Processor {
	p.decoder = NewStreamDecoder(decoder)
	return p
}

// Push consumes one inference chunk and returns the events it produced.
//
// A ready chunk received before output starts supplies the initial prompt usage
// and fingerprint. Without it, the start event uses zero prompt usage and no
// fingerprint. Later ready chunks do not update emitted metadata. Completion
// usage accumulates each processed chunk's token count, including the entire
// chunk containing a stop sequence; later chunks are not read. A missing
// tokenizer or decoder failure returns an error and ends the stream without a
// normal finish event. After the stream ends, Push returns no events.
func (p *Processor) Push(chunk InferenceChunk) ([]Event, error) {
	if p.done {
		return nil, nil
	}
	switch chunk.Kind {
	case InferenceReady:
		p.promptUsage = chunk.PromptUsage
		if !p.started {
			p.started = true
			return p.generator.Generate(StartChunk{
				SystemFingerprint: chunk.SystemFingerprint,
				Usage:             p.promptUsage,
			}), nil
		}
		return nil, nil
	case InferenceFinish:
		reason := chunk.FinishReason
		p.backendFinish = &reason
		return p.tail(), nil
	case InferenceText:
		return p.consume(chunk.Content, chunk.ContentTokens)
	default:
		if p.decoder == nil {
			p.done = true
			return nil, ErrMissingTokenizer
		}
		content, count, ok, err := p.decoder.Decode(chunk.TokenID)
		if err != nil {
			p.done = true
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return p.consume(content, count)
	}
}

// Finish processes the end of the inference source and returns the remaining
// events.
func (p *Processor) Finish() []Event {
	if p.done {
		return nil
	}
	return p.tail()
}

func (p *Processor) consume(content string, contentTokens int) ([]Event, error) {
	var events []Event
	if !p.started {
		p.started = true
		events = append(events, p.generator.Generate(StartChunk{Usage: p.promptUsage})...)
	}
	p.completionUsage.CompletionTokens += contentTokens
	actions := p.machine.Feed(content)
	p.stashed.push(content)
	events = append(events, p.stashed.applyActions(actions, p.generator)...)
	if stopSequence, ok := p.stashed.takeStopSequence(); ok {
		p.stopSequence = &stopSequence
		events = append(events, p.tail()...)
	}
	return events, nil
}

// tail completes the stream: it drains buffered parsing state, resolves the
// finish reason, and emits the finish event.
func (p *Processor) tail() []Event {
	p.done = true
	var events []Event
	if !p.started {
		p.started = true
		events = append(events, p.generator.Generate(StartChunk{Usage: p.promptUsage})...)
	}
	actions := p.machine.Finish()
	events = append(events, p.stashed.applyActions(actions, p.generator)...)
	// Combine the parts of a stop sequence matched across source chunks.
	if tail, ok := p.stashed.takeStopSequence(); ok {
		if p.stopSequence == nil {
			p.stopSequence = new(string)
		}
		*p.stopSequence += tail
	}
	var reason FinishReason
	switch {
	case p.stopSequence != nil:
		reason = FinishStopSequence
	case p.backendFinish != nil:
		switch *p.backendFinish {
		case InferenceFinishStop:
			if p.stashed.hasToolCalls {
				reason = FinishToolCalls
			} else {
				reason = FinishStop
			}
		case InferenceFinishLength:
			reason = FinishLength
		default:
			reason = FinishContentFilter
		}
	default:
		reason = FinishEndOfStream
	}
	events = append(events, p.generator.Generate(FinishChunk{
		Reason:       reason,
		StopSequence: p.stopSequence,
		Usage:        p.completionUsage,
	})...)
	return events
}

type stashedChunks struct {
	chunks            []string
	lastStashedAction *OutputActionSegment
	stashedToolName   string
	lastPopAction     OutputAction
	stopSequence      string
	hasStopSequence   bool
	hasToolCalls      bool
}

func newStashedChunks() *stashedChunks {
	return &stashedChunks{lastPopAction: OutputAction{Kind: ActionSkip}}
}

func (s *stashedChunks) push(content string) {
	s.chunks = append(s.chunks, content)
}

func (s *stashedChunks) takeStopSequence() (string, bool) {
	if !s.hasStopSequence {
		return "", false
	}
	s.hasStopSequence = false
	value := s.stopSequence
	s.stopSequence = ""
	return value, true
}

func (s *stashedChunks) pop(action OutputActionSegment) (OutputChunk, bool) {
	if len(s.chunks) == 0 {
		return nil, false
	}
	frontChunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	if action.Len < len(frontChunk) {
		s.chunks = append([]string{frontChunk[action.Len:]}, s.chunks...)
		frontChunk = frontChunk[:action.Len]
	}
	lastPopAction := s.lastPopAction
	s.lastPopAction = action.Action

	switch action.Action.Kind {
	case ActionRaw:
		if frontChunk == "" {
			return nil, false
		}
		return RawChunk{Content: frontChunk}, true
	case ActionReasoning:
		if frontChunk == "" {
			return nil, false
		}
		return ReasoningChunk{Content: frontChunk}, true
	case ActionToSpace:
		return RawChunk{Content: " "}, true
	case ActionSkip, ActionSkipInvalid:
		return nil, false
	case ActionStopSequence:
		s.stopSequence += frontChunk
		s.hasStopSequence = true
		return nil, false
	case ActionToolCallBegin:
		if lastPopAction.Kind != ActionToolCallBegin {
			return ToolCallBeginChunk{}, true
		}
		return nil, false
	case ActionToolName:
		s.stashedToolName += frontChunk
		return nil, false
	case ActionToolNameEnd:
		if lastPopAction.Kind != ActionToolNameEnd {
			s.hasToolCalls = true
			toolName := s.stashedToolName
			s.stashedToolName = ""
			return ToolCallChunk{ToolName: toolName}, true
		}
		return nil, false
	case ActionLabelToolCallArguments:
		if lastPopAction != action.Action {
			return ToolArgumentsDeltaChunk{Content: action.Action.Label}, true
		}
		return nil, false
	case ActionRawToolCallArguments:
		content := frontChunk
		if action.Action.String {
			content = escapeJSONString(content)
		}
		return ToolArgumentsDeltaChunk{Content: content}, true
	case ActionToolCallArgumentsEnd:
		if lastPopAction == action.Action {
			return nil, false
		}
		if action.Action.HasOutput {
			return ToolArgumentsDeltaChunk{Content: action.Action.Output}, true
		}
		return nil, false
	default: // ActionLabel
		if lastPopAction != action.Action {
			return RawChunk{Content: action.Action.Label}, true
		}
		return nil, false
	}
}

func (s *stashedChunks) applyWholeChunks(action *OutputActionSegment, outputs *[]OutputChunk) {
	for len(s.chunks) > 0 {
		frontLen := len(s.chunks[0])
		if frontLen > action.Len {
			return
		}
		if chunk, ok := s.pop(OutputActionSegment{Action: action.Action, Len: frontLen}); ok {
			*outputs = append(*outputs, chunk)
		}
		action.Len -= frontLen
	}
}

func (s *stashedChunks) applyActions(actions []OutputActionSegment, generator Generator) []Event {
	var outputs []OutputChunk
	lastStashedAction := s.lastStashedAction
	for _, action := range actions {
		if lastStashedAction != nil {
			if action.Action == lastStashedAction.Action {
				lastStashedAction.Len += action.Len
			} else {
				s.applyWholeChunks(lastStashedAction, &outputs)
				if lastStashedAction.Len > 0 {
					if chunk, ok := s.pop(*lastStashedAction); ok {
						outputs = append(outputs, chunk)
					}
				}
				*lastStashedAction = action
			}
		} else {
			lastStashedAction = &action
		}
	}
	if lastStashedAction != nil {
		s.applyWholeChunks(lastStashedAction, &outputs)
	}
	s.lastStashedAction = lastStashedAction

	var events []Event
	for _, output := range outputs {
		events = append(events, generator.Generate(output)...)
	}
	return events
}

func escapeJSONString(content string) string {
	escaped, err := jsonx.MarshalString(content)
	if err != nil || len(escaped) <= 2 {
		return ""
	}
	return escaped[1 : len(escaped)-1]
}
