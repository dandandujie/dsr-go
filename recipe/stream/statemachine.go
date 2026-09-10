// Package stream parses model output and turns it into protocol response
// events.
package stream

// ReasoningStage is the position within the reasoning section when parsing
// begins.
type ReasoningStage uint8

const (
	// ReasoningStageStart begins reasoning while discarding leading newlines.
	ReasoningStageStart ReasoningStage = iota
	// ReasoningStageReasoning continues reasoning while preserving leading
	// newlines.
	ReasoningStageReasoning
	// ReasoningStageContent continues answer content while preserving leading
	// newlines.
	ReasoningStageContent
)

// StartFromReasoning reports whether the stage is inside the reasoning section.
func (s ReasoningStage) StartFromReasoning() bool {
	return s == ReasoningStageStart || s == ReasoningStageReasoning
}

const (
	jsonBeginLabel = "\x60\x60\x60json\n"
	jsonFenceLabel = "\x60\x60\x60\n"
	jsonEndLabel   = "\n\x60\x60\x60"
	dsmlBeginLabel = "<｜DSML｜"
	dsmlEndLabel   = "</｜DSML｜"
	// ReasoningEndLabel ends the reasoning section of model output.
	ReasoningEndLabel = "</think>"
)

var (
	rawJSONBeginLabels     = []string{"{", "["}
	jsonBeginLabels        = []string{jsonBeginLabel, jsonFenceLabel}
	dsmlToolCallsEndLabels = []string{"</｜DSML｜tool_calls>", "</｜DSML｜ calls>"}
)

// ParsingOptions configures output parsing.
type ParsingOptions struct {
	// ParseToolCalls parses model-specific tool-call markup.
	ParseToolCalls bool
	// ToolCallInitialStage begins parsing inside the tool-call block specified
	// by the prompt. It takes precedence over ReasoningInitialStage.
	ToolCallInitialStage bool
	// ParseJSONOutput recognizes JSON fences or a raw "{" or "[" prefix and
	// replaces surrounding text with spaces. It does not validate JSON syntax.
	ParseJSONOutput bool
	// ReasoningInitialStage is the initial reasoning or answer stage. A nil
	// stage starts answer content with leading-newline removal.
	ReasoningInitialStage *ReasoningStage
	// StopSequences matches stop sequences in ordinary answer text and detected
	// JSON output. Reasoning, tool-call markup, and surrounding JSON-mode text
	// are excluded. Empty sequences are discarded during construction.
	StopSequences []string
}

// DefaultParsingOptions returns the parsing defaults: tool-call parsing
// enabled, no JSON parsing, no stop sequences.
func DefaultParsingOptions() ParsingOptions {
	return ParsingOptions{ParseToolCalls: true}
}

// InvalidTokenKind classifies discarded model output.
type InvalidTokenKind uint8

const (
	// InvalidExtraEndOfThinking is a reasoning end marker outside reasoning.
	InvalidExtraEndOfThinking InvalidTokenKind = iota
	// InvalidContentAfterFinished is output after the response finished.
	InvalidContentAfterFinished
)

// ActionKind identifies one output action.
type ActionKind uint8

const (
	// ActionRaw emits ordinary answer content.
	ActionRaw ActionKind = iota
	// ActionToSpace emits a single space in JSON mode.
	ActionToSpace
	// ActionSkipInvalid discards content and records why.
	ActionSkipInvalid
	// ActionSkip discards content.
	ActionSkip
	// ActionStopSequence discards a matched stop sequence.
	ActionStopSequence
	// ActionToolCallBegin starts a tool call.
	ActionToolCallBegin
	// ActionToolName collects the tool name.
	ActionToolName
	// ActionToolNameEnd ends the tool name.
	ActionToolNameEnd
	// ActionLabelToolCallArguments emits a fixed arguments fragment.
	ActionLabelToolCallArguments
	// ActionRawToolCallArguments emits raw arguments content.
	ActionRawToolCallArguments
	// ActionToolCallArgumentsEnd ends the tool arguments.
	ActionToolCallArgumentsEnd
	// ActionReasoning emits reasoning content.
	ActionReasoning
	// ActionLabel emits a fixed output fragment.
	ActionLabel
)

// OutputAction is one parsing action. It is comparable with ==.
type OutputAction struct {
	// Kind selects the action.
	Kind ActionKind
	// InvalidKind is set for ActionSkipInvalid.
	InvalidKind InvalidTokenKind
	// Label is set for ActionLabel and ActionLabelToolCallArguments.
	Label string
	// ID identifies the argument fragment of ActionLabelToolCallArguments.
	ID uint32
	// String marks a quoted value for ActionRawToolCallArguments.
	String bool
	// Output and HasOutput carry the fixed arguments of
	// ActionToolCallArgumentsEnd.
	Output    string
	HasOutput bool
}

// OutputActionSegment is one action over len input bytes.
type OutputActionSegment struct {
	// Action is the parsing action.
	Action OutputAction
	// Len is the number of input bytes the action covers.
	Len int
}

// StateMachine is an incremental parser whose output segments refer to input
// byte lengths.
type StateMachine struct {
	options ParsingOptions
	state   *machineState
}

// NewStateMachine builds a state machine from parsing options.
func NewStateMachine(options ParsingOptions) *StateMachine {
	stopSequences := make([]string, 0, len(options.StopSequences))
	for _, sequence := range options.StopSequences {
		if sequence != "" {
			stopSequences = append(stopSequences, sequence)
		}
	}
	options.StopSequences = stopSequences
	var first stage
	switch {
	case options.ToolCallInitialStage:
		first = stage{kind: stageToolCalls}
	case options.ReasoningInitialStage != nil:
		switch *options.ReasoningInitialStage {
		case ReasoningStageStart:
			first = stage{kind: stageReasoning, flag: true}
		case ReasoningStageReasoning:
			first = stage{kind: stageReasoning}
		default:
			first = stage{kind: stageCommon}
		}
	default:
		first = stage{kind: stageCommon, flag: true}
	}
	return &StateMachine{options: options, state: newMachineState(first, &options)}
}

// Feed parses the next source chunk, retaining incomplete markers for later
// input.
func (m *StateMachine) Feed(content string) []OutputActionSegment {
	return m.state.feed(content, &m.options)
}

// Finish processes buffered input when the source ends.
func (m *StateMachine) Finish() []OutputActionSegment {
	return m.state.finish()
}

type stageKind uint8

const (
	stageCommon stageKind = iota
	stageJSON
	stageMatchedJSON
	stageToolCalls
	stageToolName
	stageToolCallArguments
	stageToolCallParamName
	stageToolCallParamType
	stageToolCallParamValue
	stageReasoning
	stageFinished
)

// stage is one parser stage. flag carries is_leading for common, reasoning and
// tool-call-argument stages, and string for parameter values.
type stage struct {
	kind stageKind
	flag bool
}

type matchBranch struct {
	state           matchState
	nextStage       stage
	actionOnMatched OutputAction
}

type machineState struct {
	stage             stage
	branches          []matchBranch
	actionOnUnmatched OutputAction
	stashedSize       int
}

// newMachineState builds the branch table of one stage.
func newMachineState(s stage, options *ParsingOptions) *machineState {
	state := &machineState{stage: s}
	addStopSequenceBranches := func() {
		for _, label := range options.StopSequences {
			state.addStringBranch(label, stage{kind: stageFinished}, OutputAction{Kind: ActionStopSequence})
		}
	}
	addToolCallBeginBranches := func() {
		state.addBranch(matchPattern{kind: patternNewlinesAndString, value: dsmlBeginLabel},
			stage{kind: stageToolCalls}, OutputAction{Kind: ActionSkip})
	}

	switch s.kind {
	case stageCommon:
		if options.ParseJSONOutput {
			for _, label := range jsonBeginLabels {
				state.addStringBranch(label, stage{kind: stageJSON}, OutputAction{Kind: ActionSkip})
			}
			for _, label := range rawJSONBeginLabels {
				state.addStringBranch(label, stage{kind: stageJSON}, OutputAction{Kind: ActionLabel, Label: label})
			}
		} else {
			addStopSequenceBranches()
		}
		if options.ParseToolCalls {
			addToolCallBeginBranches()
		}
		if s.flag {
			state.addBranch(matchPattern{kind: patternLeadingNewline},
				stage{kind: stageCommon, flag: true}, OutputAction{Kind: ActionSkip})
		}
		state.addStringBranch(ReasoningEndLabel, stage{kind: stageCommon},
			OutputAction{Kind: ActionSkipInvalid, InvalidKind: InvalidExtraEndOfThinking})
		if options.ParseJSONOutput {
			state.actionOnUnmatched = OutputAction{Kind: ActionToSpace}
		} else {
			state.actionOnUnmatched = OutputAction{Kind: ActionRaw}
		}
	case stageJSON:
		addStopSequenceBranches()
		state.addStringBranch(jsonEndLabel, stage{kind: stageMatchedJSON}, OutputAction{Kind: ActionSkip})
		state.actionOnUnmatched = OutputAction{Kind: ActionRaw}
	case stageMatchedJSON:
		if options.ParseToolCalls {
			addToolCallBeginBranches()
		}
		state.actionOnUnmatched = OutputAction{Kind: ActionToSpace}
	case stageToolCalls:
		state.addStringBranch("invoke name=\"", stage{kind: stageToolName},
			OutputAction{Kind: ActionToolCallBegin})
		for _, label := range dsmlToolCallsEndLabels {
			state.addStringBranch(label, stage{kind: stageFinished}, OutputAction{Kind: ActionSkip})
		}
		state.actionOnUnmatched = OutputAction{Kind: ActionSkip}
	case stageToolName:
		state.addStringBranch("\"", stage{kind: stageToolCallArguments, flag: true},
			OutputAction{Kind: ActionToolNameEnd})
		state.actionOnUnmatched = OutputAction{Kind: ActionToolName}
	case stageToolCallArguments:
		if s.flag {
			state.addStringBranch("parameter name=", stage{kind: stageToolCallParamName},
				OutputAction{Kind: ActionLabelToolCallArguments, Label: "{", ID: 0})
			state.addStringBranch(dsmlEndLabel, stage{kind: stageToolCalls},
				OutputAction{Kind: ActionToolCallArgumentsEnd, Output: "{}", HasOutput: true})
		} else {
			state.addStringBranch("parameter name=", stage{kind: stageToolCallParamName},
				OutputAction{Kind: ActionLabelToolCallArguments, Label: ", ", ID: 1})
			state.addStringBranch(dsmlEndLabel, stage{kind: stageToolCalls},
				OutputAction{Kind: ActionToolCallArgumentsEnd, Output: "}", HasOutput: true})
		}
		state.actionOnUnmatched = OutputAction{Kind: ActionSkip}
	case stageToolCallParamName:
		state.addStringBranch(" ", stage{kind: stageToolCallParamType},
			OutputAction{Kind: ActionLabelToolCallArguments, Label: ": ", ID: 2})
		state.actionOnUnmatched = OutputAction{Kind: ActionRawToolCallArguments}
	case stageToolCallParamType:
		state.addStringBranch("true\">", stage{kind: stageToolCallParamValue, flag: true},
			OutputAction{Kind: ActionLabelToolCallArguments, Label: "\"", ID: 3})
		state.addStringBranch("false\">", stage{kind: stageToolCallParamValue},
			OutputAction{Kind: ActionSkip})
		state.actionOnUnmatched = OutputAction{Kind: ActionSkip}
	case stageToolCallParamValue:
		if s.flag {
			state.addStringBranch(dsmlEndLabel, stage{kind: stageToolCallArguments},
				OutputAction{Kind: ActionLabelToolCallArguments, Label: "\"", ID: 4})
		} else {
			state.addStringBranch(dsmlEndLabel, stage{kind: stageToolCallArguments},
				OutputAction{Kind: ActionSkip})
		}
		state.actionOnUnmatched = OutputAction{Kind: ActionRawToolCallArguments, String: s.flag}
	case stageReasoning:
		if options.ParseToolCalls {
			addToolCallBeginBranches()
		}
		if s.flag {
			state.addBranch(matchPattern{kind: patternLeadingNewline},
				stage{kind: stageReasoning, flag: s.flag}, OutputAction{Kind: ActionSkip})
		}
		state.addBranch(matchPattern{kind: patternNewlinesAndString, value: ReasoningEndLabel},
			stage{kind: stageCommon, flag: true}, OutputAction{Kind: ActionSkip})
		state.actionOnUnmatched = OutputAction{Kind: ActionReasoning}
	case stageFinished:
		state.actionOnUnmatched = OutputAction{Kind: ActionSkipInvalid, InvalidKind: InvalidContentAfterFinished}
	}
	return state
}

func (s *machineState) addStringBranch(label string, next stage, action OutputAction) {
	s.addBranch(matchPattern{kind: patternString, value: label}, next, action)
}

func (s *machineState) addBranch(pattern matchPattern, next stage, action OutputAction) {
	s.branches = append(s.branches, matchBranch{
		state:           pattern.newState(),
		nextStage:       next,
		actionOnMatched: action,
	})
}

func (s *machineState) feed(content string, options *ParsingOptions) []OutputActionSegment {
	var actions []OutputActionSegment
	for index := 0; index < len(content); index++ {
		byteValue := content[index]
		s.stashedSize++
		for i := range s.branches {
			branch := &s.branches[i]
			if !branch.state.feed(byteValue) {
				continue
			}
			matchingLen := branch.state.matchingLen()
			if matchingLen < s.stashedSize {
				actions = append(actions, OutputActionSegment{
					Action: s.actionOnUnmatched,
					Len:    s.stashedSize - matchingLen,
				})
			}
			if matchingLen > 0 {
				actions = append(actions, OutputActionSegment{
					Action: branch.actionOnMatched,
					Len:    matchingLen,
				})
			}
			next := newMachineState(branch.nextStage, options)
			*s = *next
			break
		}
	}
	matchingLen := 0
	for i := range s.branches {
		if length := s.branches[i].state.matchingLen(); length > matchingLen {
			matchingLen = length
		}
	}
	if matchingLen < s.stashedSize {
		actions = append(actions, OutputActionSegment{
			Action: s.actionOnUnmatched,
			Len:    s.stashedSize - matchingLen,
		})
		s.stashedSize = matchingLen
	}
	return actions
}

func (s *machineState) finish() []OutputActionSegment {
	var actions []OutputActionSegment
	if s.stashedSize > 0 {
		actions = append(actions, OutputActionSegment{Action: s.actionOnUnmatched, Len: s.stashedSize})
	}
	return actions
}

type patternKind uint8

const (
	patternString patternKind = iota
	patternNewlinesAndString
	patternLeadingNewline
)

type matchPattern struct {
	kind  patternKind
	value string
}

func (p matchPattern) newState() matchState {
	switch p.kind {
	case patternString:
		return matchState{kind: matchString, value: p.value, kmpTable: kmpTable(p.value)}
	case patternNewlinesAndString:
		return matchState{kind: matchNewlinesAndString, value: p.value, kmpTable: kmpTable(p.value)}
	default:
		return matchState{kind: matchLeadingNewline, leading: true}
	}
}

type matchKind uint8

const (
	matchString matchKind = iota
	matchNewlinesAndString
	matchLeadingNewline
)

type matchState struct {
	kind         matchKind
	value        string
	kmpTable     []int
	length       int
	newlineCount int
	leading      bool
	matched      bool
}

func kmpTable(pattern string) []int {
	table := make([]int, len(pattern))
	length := 0
	for i := 1; i < len(pattern); {
		switch {
		case pattern[i] == pattern[length]:
			length++
			table[i] = length
			i++
		case length != 0:
			length = table[length-1]
		default:
			table[i] = 0
			i++
		}
	}
	return table
}

func kmpNext(pattern string, table []int, length int, byteValue byte) int {
	for length > 0 && (length >= len(pattern) || pattern[length] != byteValue) {
		length = table[length-1]
	}
	if length < len(pattern) && pattern[length] == byteValue {
		length++
	}
	return length
}

func (m *matchState) feed(byteValue byte) bool {
	switch m.kind {
	case matchString:
		m.length = kmpNext(m.value, m.kmpTable, m.length, byteValue)
		return m.length == len(m.value)
	case matchNewlinesAndString:
		if m.length == 0 && byteValue == '\n' {
			m.newlineCount++
		} else {
			next := kmpNext(m.value, m.kmpTable, m.length, byteValue)
			if next <= m.length {
				m.newlineCount = 0
			}
			m.length = next
		}
		return m.length == len(m.value)
	default:
		if m.leading {
			m.matched = byteValue == '\n'
		}
		m.leading = false
		return m.matched
	}
}

func (m *matchState) matchingLen() int {
	switch m.kind {
	case matchString:
		return m.length
	case matchNewlinesAndString:
		return m.length + m.newlineCount
	default:
		if m.matched {
			return 1
		}
		return 0
	}
}
