//! Reference generator: reads golden cases from stdin (one JSON object per
//! line) and writes reference results to stdout, for differential testing of
//! the Go rewrite.

use std::io::{self, BufRead, Write};
use std::pin::pin;

use deepseek_recipe::anthropic::MessagesRequest;
use deepseek_recipe::openai::responses::response::ResponsesResponse;
use deepseek_recipe::openai::{ChatCompletionRequest, ResponsesRequest};
use deepseek_recipe::request::{ConversionError, ConversationRequest, ConversionOptions, ProtocolRequest};
use deepseek_recipe::response::ProtocolResponse;
use deepseek_recipe::stream::{
    ChunkGenerator, InferenceChunk, InferenceFinishReason, PromptUsage, StreamProcessor,
};
use deepseek_recipe::util::append_delta::AppendDelta;
use deepseek_recipe_core::conversation::{Conversation, ReasoningEffort, ResponseFormat};
use deepseek_recipe_core::messages::InputMessage;
use deepseek_recipe_core::multimodal::{ImageDetail, ImageInfo, ImageSource};
use deepseek_recipe_core::tools::ToolChoice;
use deepseek_recipe_core::util::json_formatter::stringify_python_style;
use deepseek_recipe_encoding::PromptEncoding;
use deepseek_recipe_encoding::v4::dsv4::DeepseekV4Encoding;
use deepseek_recipe_encoding::v4::dsv41::DeepseekV41Encoding;
use deepseek_recipe_image::{
    ImageByteBudget, ImageError, ImageFetcher, ImagePreprocessor, ImageQuota, ImageResolver,
    PreprocessOptions, RetryPolicy,
};
use std::time::Duration;
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tokenizers::Tokenizer;
use tokio_stream::{iter, StreamExt};

#[derive(Deserialize)]
struct Case {
    name: String,
    op: String,
    #[serde(default)]
    protocol: Option<String>,
    #[serde(default)]
    body: Option<Value>,
    #[serde(default)]
    chunks: Option<Vec<ChunkSpec>>,
    #[serde(default)]
    tokenizer: Option<String>,
    #[serde(default)]
    value: Option<Value>,
    #[serde(default)]
    text: Option<String>,
    #[serde(default)]
    ids: Option<Vec<u32>>,
    #[serde(default)]
    numbers: Option<Vec<String>>,
    #[serde(default)]
    skip_special_tokens: Option<bool>,
    #[serde(default)]
    encoding: Option<String>,
    #[serde(default)]
    sources: Option<Vec<SourceSpec>>,
}

#[derive(Clone, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
enum SourceSpec {
    DataUrl {
        data_url: String,
        #[serde(default)]
        detail: Option<String>,
    },
    Url {
        url: String,
        #[serde(default)]
        detail: Option<String>,
    },
    Bytes {
        hex: String,
        #[serde(default)]
        detail: Option<String>,
    },
}

#[derive(Clone, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
enum ChunkSpec {
    Ready {
        #[serde(default)]
        system_fingerprint: Option<String>,
        #[serde(default)]
        prompt_tokens: usize,
        #[serde(default)]
        prompt_cache_hit_tokens: usize,
    },
    Text {
        content: String,
        #[serde(default)]
        content_tokens: usize,
    },
    Token {
        token_id: u32,
    },
    TokenText {
        text: String,
    },
    Finish {
        finish_reason: String,
    },
}

fn inference_chunks(
    specs: &[ChunkSpec],
    tokenizer: Option<&Tokenizer>,
) -> Result<Vec<InferenceChunk>, ConversionError> {
    let mut chunks = Vec::new();
    for spec in specs {
        match spec {
            ChunkSpec::TokenText { text } => {
                let tokenizer = tokenizer.ok_or_else(|| {
                    ConversionError::bad_request("token_text chunk requires a tokenizer")
                })?;
                let encoding = tokenizer
                    .encode(text.as_str(), false)
                    .map_err(|error| ConversionError::internal(error.to_string()))?;
                for id in encoding.get_ids() {
                    chunks.push(InferenceChunk::Token { token_id: *id });
                }
            }
            other => chunks.push(convert_chunk(other)),
        }
    }
    Ok(chunks)
}

fn convert_chunk(spec: &ChunkSpec) -> InferenceChunk {
    match spec {
            ChunkSpec::Ready {
                system_fingerprint,
                prompt_tokens,
                prompt_cache_hit_tokens,
            } => InferenceChunk::Ready {
                system_fingerprint: system_fingerprint.clone(),
                prompt_usage: PromptUsage {
                    prompt_tokens: *prompt_tokens,
                    prompt_cache_hit_tokens: *prompt_cache_hit_tokens,
                },
            },
            ChunkSpec::Text {
                content,
                content_tokens,
            } => InferenceChunk::Text {
                content: content.clone(),
                content_tokens: *content_tokens,
            },
            ChunkSpec::Token { token_id } => InferenceChunk::Token { token_id: *token_id },
            ChunkSpec::TokenText { text } => InferenceChunk::Token {
                token_id: text.len() as u32,
            },
            ChunkSpec::Finish { finish_reason } => InferenceChunk::Finish {
                finish_reason: match finish_reason.as_str() {
                    "length" => InferenceFinishReason::Length,
                    "content_filter" => InferenceFinishReason::ContentFilter,
                    _ => InferenceFinishReason::Stop,
                },
            },
        }
}

fn detail_name(detail: ImageDetail) -> &'static str {
    match detail {
        ImageDetail::Low => "low",
        ImageDetail::High => "high",
        ImageDetail::Original => "original",
        ImageDetail::Auto => "auto",
    }
}

fn dump_images(sources: &[ImageSource]) -> Value {
    Value::Array(
        sources
            .iter()
            .map(|source| match source {
                ImageSource::DataUrl { data_url, detail } => json!({
                    "kind": "data_url",
                    "data_url": data_url,
                    "detail": detail_name(*detail),
                }),
                ImageSource::Url { url, detail } => json!({
                    "kind": "url",
                    "url": url,
                    "detail": detail_name(*detail),
                }),
                ImageSource::Bytes { data, detail } => json!({
                    "kind": "bytes",
                    "len": data.len(),
                    "detail": detail_name(*detail),
                }),
            })
            .collect(),
    )
}

fn dump_message(message: &InputMessage) -> Value {
    match message {
        InputMessage::System { content } => json!({"role": "system", "content": content}),
        InputMessage::User {
            content,
            image_sources,
        } => json!({
            "role": "user",
            "content": content,
            "images": dump_images(image_sources),
        }),
        InputMessage::Assistant {
            content,
            reasoning_content,
            tool_calls,
        } => json!({
            "role": "assistant",
            "content": content,
            "reasoning_content": match reasoning_content.as_deref() {
                Some(text) if !text.is_empty() => json!(text),
                _ => Value::Null,
            },
            "tool_calls": tool_calls.as_ref().map(|calls| Value::Array(
                calls
                    .iter()
                    .map(|call| json!({
                        "id": call.id,
                        "name": call.name,
                        "arguments": call.arguments,
                    }))
                    .collect(),
            )),
        }),
        InputMessage::Tool {
            content,
            image_sources,
            tool_call_id,
        } => json!({
            "role": "tool",
            "content": content,
            "tool_call_id": tool_call_id,
            "images": dump_images(image_sources),
        }),
        InputMessage::LatestReminder { content } => {
            json!({"role": "latest_reminder", "content": content})
        }
    }
}

fn dump_conversation(conversation: &Conversation) -> Value {
    json!({
        "messages": conversation.messages.iter().map(dump_message).collect::<Vec<_>>(),
        "thinking_mode": conversation.thinking_mode,
        "tools": conversation.tools.iter().map(|tool| json!({
            "name": tool.name,
            "description": tool.description,
            "parameters": tool.parameters,
            "strict": tool.strict,
        })).collect::<Vec<_>>(),
        "tool_choice": match conversation.tool_choice {
            ToolChoice::Auto => "auto",
            ToolChoice::None => "none",
            ToolChoice::Required => "required",
        },
        "reasoning_effort": conversation.reasoning_effort.map(|effort| match effort {
            ReasoningEffort::Low => "low",
            ReasoningEffort::High => "high",
            ReasoningEffort::Xhigh => "xhigh",
            ReasoningEffort::Max => "max",
        }),
        "response_format": match conversation.response_format {
            ResponseFormat::Text => "text",
            ResponseFormat::JsonObject => "json_object",
        },
    })
}

fn dump_conversion(request: &ConversationRequest) -> Value {
    json!({
        "conversation": dump_conversation(&request.conversation),
        "inference_options": {
            "max_tokens": request.inference_options.max_tokens,
            "temperature": request.inference_options.temperature,
            "top_p": request.inference_options.top_p,
            "thinking_budget_tokens": request.inference_options.thinking_budget_tokens,
            "disable_parallel_tool_use": request.inference_options.disable_parallel_tool_use,
        },
        "parsing_options": {
            "parse_tool_calls": request.parsing_options.parse_tool_calls,
            "tool_call_initial_stage": request.parsing_options.tool_call_initial_stage,
            "parse_json_output": request.parsing_options.parse_json_output,
            "reasoning_initial_stage": request.parsing_options.reasoning_initial_stage.map(|stage| match stage {
                deepseek_recipe::stream::state_machine::ReasoningStage::Start => "start",
                deepseek_recipe::stream::state_machine::ReasoningStage::Reasoning => "reasoning",
                deepseek_recipe::stream::state_machine::ReasoningStage::Content => "content",
            }),
            "stop_sequences": request.parsing_options.stop_sequences,
        },
        "model": request.model,
        "stream": request.stream,
    })
}

fn convert<T>(body: Value) -> Result<ConversationRequest, ConversionError>
where
    T: ProtocolRequest + DeserializeOwned,
{
    let request: T = serde_json::from_value(body)
        .map_err(|error| ConversionError::bad_request(format!("invalid request body: {error}")))?;
    request.convert(ConversionOptions::default())
}

fn load_tokenizer(name: &str) -> Result<Tokenizer, ConversionError> {
    use std::collections::HashMap;
    use std::sync::{Mutex, OnceLock};

    static CACHE: OnceLock<Mutex<HashMap<String, Tokenizer>>> = OnceLock::new();
    let cache = CACHE.get_or_init(|| Mutex::new(HashMap::new()));
    let mut guard = cache.lock().expect("tokenizer cache lock");
    if let Some(tokenizer) = guard.get(name) {
        return Ok(tokenizer.clone());
    }
    let path = format!(
        "{}/../../static/tokenizers/{name}/tokenizer.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let tokenizer = Tokenizer::from_file(path)
        .map_err(|error| ConversionError::internal(error.to_string()))?;
    guard.insert(name.to_string(), tokenizer.clone());
    Ok(tokenizer)
}

fn failure(error: ConversionError) -> Value {
    json!({
        "ok": false,
        "error": {
            "kind": match error {
                ConversionError::BadRequest { .. } => "bad_request",
                ConversionError::Internal { .. } => "internal",
            },
            "detail": error.to_string(),
        },
    })
}

fn success(result: Value) -> Value {
    json!({"ok": true, "result": result})
}

fn run_text<T>(body: Value, encoding_name: &str) -> Result<Value, ConversionError>
where
    T: ProtocolRequest + DeserializeOwned,
{
    let request = convert::<T>(body)?;
    let rendered = if encoding_name == "v4" {
        DeepseekV4Encoding::new().render_conversation(&request.conversation)
    } else {
        DeepseekV41Encoding::new().render_conversation(&request.conversation)
    };
    Ok(json!({
        "prompt": rendered.prompt,
        "image_sources": dump_images(&rendered.image_sources),
    }))
}

fn run_encode<T>(body: Value, tokenizer: &str, encoding_name: &str) -> Result<Value, ConversionError>
where
    T: ProtocolRequest + DeserializeOwned,
{
    let request = convert::<T>(body)?;
    let tokenizer = load_tokenizer(tokenizer)?;
    let ids = if encoding_name == "v4" {
        DeepseekV4Encoding::new()
            .with_tokenizer(tokenizer)
            .encode(&request.conversation)
    } else {
        DeepseekV41Encoding::new()
            .with_tokenizer(tokenizer)
            .encode(&request.conversation)
    }
    .map_err(|error| ConversionError::internal(error.to_string()))?;
    Ok(json!({"ids": ids}))
}

async fn run_stream<T>(body: Value, specs: &[ChunkSpec], tokenizer: Option<&str>) -> Result<Value, ConversionError>
where
    T: ProtocolRequest + DeserializeOwned,
    <<T::Response as ProtocolResponse>::ChunkGenerator as ChunkGenerator>::Chunk: Serialize + Send,
{
    let request = convert::<T>(body)?;
    let generator = T::chunk_generator(&request, "mock-id".to_string(), "deepseek-flash".to_string());
    let mut processor = StreamProcessor::new(generator, request.parsing_options);
    let mut decoder_tokenizer = None;
    if let Some(tokenizer) = tokenizer {
        let loaded = load_tokenizer(tokenizer)?;
        processor = processor.with_tokenizer(loaded.clone());
        decoder_tokenizer = Some(loaded);
    }
    let inference = inference_chunks(specs, decoder_tokenizer.as_ref())?;
    let mut chunks = pin!(processor.process(iter(inference)));
    let mut events = Vec::new();
    while let Some(chunk) = chunks.next().await {
        let chunk = chunk.map_err(|error| ConversionError::internal(error.to_string()))?;
        events.push(serde_json::to_value(&chunk).map_err(|error| {
            ConversionError::internal(error.to_string())
        })?);
    }
    Ok(json!({"events": events}))
}

async fn run_complete<T>(body: Value, specs: &[ChunkSpec]) -> Result<Value, ConversionError>
where
    T: ProtocolRequest + DeserializeOwned,
    <<T::Response as ProtocolResponse>::ChunkGenerator as ChunkGenerator>::Chunk: Serialize + Send,
{
    let request = convert::<T>(body)?;
    let generator = T::chunk_generator(&request, "mock-id".to_string(), "deepseek-flash".to_string());
    let processor = StreamProcessor::new(generator, request.parsing_options);
    let mut response = T::Response::new("mock-id".to_string(), "deepseek-flash".to_string(), 0, 0, 0);
    let inference = inference_chunks(specs, None)?;
    let mut chunks = pin!(processor.process(iter(inference)));
    while let Some(chunk) = chunks.next().await {
        let chunk = chunk.map_err(|error| ConversionError::internal(error.to_string()))?;
        response.append(chunk);
    }
    serde_json::to_value(response)
        .map_err(|error| ConversionError::internal(error.to_string()))
}


// The Responses example server applies the request custom tool declarations to
// the generator; these paths mirror that so custom tool output is covered.
async fn run_stream_responses(
    body: Value,
    specs: &[ChunkSpec],
    tokenizer: Option<&str>,
) -> Result<Value, ConversionError> {
    let typed: ResponsesRequest = serde_json::from_value(body).map_err(|error| {
        ConversionError::bad_request(format!("invalid request body: {error}"))
    })?;
    let custom_tool_names = typed.custom_tool_names();
    let request = typed.convert(ConversionOptions::default())?;
    let generator = ResponsesRequest::chunk_generator(
        &request,
        "mock-id".to_string(),
        "deepseek-flash".to_string(),
    )
    .with_custom_tool_names(custom_tool_names);
    let mut processor = StreamProcessor::new(generator, request.parsing_options);
    let mut decoder_tokenizer = None;
    if let Some(tokenizer) = tokenizer {
        let loaded = load_tokenizer(tokenizer)?;
        processor = processor.with_tokenizer(loaded.clone());
        decoder_tokenizer = Some(loaded);
    }
    let inference = inference_chunks(specs, decoder_tokenizer.as_ref())?;
    let mut chunks = pin!(processor.process(iter(inference)));
    let mut events = Vec::new();
    while let Some(chunk) = chunks.next().await {
        let chunk = chunk.map_err(|error| ConversionError::internal(error.to_string()))?;
        events.push(
            serde_json::to_value(&chunk)
                .map_err(|error| ConversionError::internal(error.to_string()))?,
        );
    }
    Ok(json!({"events": events}))
}

async fn run_complete_responses(
    body: Value,
    specs: &[ChunkSpec],
) -> Result<Value, ConversionError> {
    let typed: ResponsesRequest = serde_json::from_value(body).map_err(|error| {
        ConversionError::bad_request(format!("invalid request body: {error}"))
    })?;
    let custom_tool_names = typed.custom_tool_names();
    let request = typed.convert(ConversionOptions::default())?;
    let generator = ResponsesRequest::chunk_generator(
        &request,
        "mock-id".to_string(),
        "deepseek-flash".to_string(),
    )
    .with_custom_tool_names(custom_tool_names);
    let processor = StreamProcessor::new(generator, request.parsing_options);
    let mut response = ResponsesResponse::new(
        "mock-id".to_string(),
        "deepseek-flash".to_string(),
        0,
        0,
        0,
    );
    let inference = inference_chunks(specs, None)?;
    let mut chunks = pin!(processor.process(iter(inference)));
    while let Some(chunk) = chunks.next().await {
        let chunk = chunk.map_err(|error| ConversionError::internal(error.to_string()))?;
        response.append(chunk);
    }
    serde_json::to_value(response)
        .map_err(|error| ConversionError::internal(error.to_string()))
}

macro_rules! with_protocol {
    ($protocol:expr, $call:ident $(, $arg:expr)*) => {
        match $protocol.as_str() {
            "chat_completions" => $call::<ChatCompletionRequest>($($arg),*).await,
            "responses" => $call::<ResponsesRequest>($($arg),*).await,
            "messages" => $call::<MessagesRequest>($($arg),*).await,
            other => Err(ConversionError::bad_request(format!("unknown protocol: {other}"))),
        }
    };
}

async fn handle(case: Case) -> Value {
    let protocol = case.protocol.clone().unwrap_or_default();
    let outcome: Result<Value, ConversionError> = match case.op.as_str() {
        "convert" => {
            let body = case.body.clone().unwrap_or(Value::Null);
            let convert_call = |body: Value| async move {
                match protocol.as_str() {
                    "chat_completions" => convert::<ChatCompletionRequest>(body).map(|r| dump_conversion(&r)),
                    "responses" => convert::<ResponsesRequest>(body).map(|r| dump_conversion(&r)),
                    "messages" => convert::<MessagesRequest>(body).map(|r| dump_conversion(&r)),
                    other => Err(ConversionError::bad_request(format!("unknown protocol: {other}"))),
                }
            };
            convert_call(body).await
        }
        "render" => {
            let encoding = case.encoding.clone().unwrap_or_else(|| "v41".to_string());
            match protocol.as_str() {
                "chat_completions" => run_text::<ChatCompletionRequest>(case.body.clone().unwrap_or(Value::Null), &encoding),
                "responses" => run_text::<ResponsesRequest>(case.body.clone().unwrap_or(Value::Null), &encoding),
                "messages" => run_text::<MessagesRequest>(case.body.clone().unwrap_or(Value::Null), &encoding),
                other => Err(ConversionError::bad_request(format!("unknown protocol: {other}"))),
            }
        }
        "encode" => {
            let encoding = case.encoding.clone().unwrap_or_else(|| "v41".to_string());
            let tokenizer = case.tokenizer.clone().unwrap_or_else(|| encoding.clone());
            match protocol.as_str() {
                "chat_completions" => run_encode::<ChatCompletionRequest>(case.body.clone().unwrap_or(Value::Null), &tokenizer, &encoding),
                "responses" => run_encode::<ResponsesRequest>(case.body.clone().unwrap_or(Value::Null), &tokenizer, &encoding),
                "messages" => run_encode::<MessagesRequest>(case.body.clone().unwrap_or(Value::Null), &tokenizer, &encoding),
                other => Err(ConversionError::bad_request(format!("unknown protocol: {other}"))),
            }
        }
        "stream" => {
            let specs = case.chunks.clone().unwrap_or_default();
            let tokenizer = case.tokenizer.clone();
            if protocol == "responses" {
                run_stream_responses(
                    case.body.clone().unwrap_or(Value::Null),
                    &specs,
                    tokenizer.as_deref(),
                )
                .await
            } else {
                with_protocol!(protocol, run_stream, case.body.clone().unwrap_or(Value::Null), &specs, tokenizer.as_deref())
            }
        }
        "complete" => {
            let specs = case.chunks.clone().unwrap_or_default();
            if protocol == "responses" {
                run_complete_responses(case.body.clone().unwrap_or(Value::Null), &specs).await
            } else {
                with_protocol!(protocol, run_complete, case.body.clone().unwrap_or(Value::Null), &specs)
            }
        }
        "image" => run_image(&case).await,
        "tokenize" => match load_tokenizer(&case.tokenizer.clone().unwrap_or_else(|| "v41".to_string())) {
            Ok(tokenizer) => tokenizer
                .encode(case.text.clone().unwrap_or_default(), false)
                .map(|encoding| json!({"ids": encoding.get_ids()}))
                .map_err(|error| ConversionError::internal(error.to_string())),
            Err(error) => Err(error),
        },
        "detokenize" => match load_tokenizer(&case.tokenizer.clone().unwrap_or_else(|| "v41".to_string())) {
            Ok(tokenizer) => tokenizer
                .decode(&case.ids.clone().unwrap_or_default(), case.skip_special_tokens.unwrap_or(false))
                .map(|text| json!({"text": text}))
                .map_err(|error| ConversionError::internal(error.to_string())),
            Err(error) => Err(error),
        },
        "stringify" => Ok(json!({
            "text": stringify_python_style(&case.value.clone().unwrap_or(Value::Null)),
        })),
        "numbers" => (|| -> Result<Value, ConversionError> {
            let mut compact = Vec::new();
            let mut python = Vec::new();
            for number in case.numbers.clone().unwrap_or_default() {
                let value: Value = serde_json::from_str(&number)
                    .map_err(|error| ConversionError::bad_request(error.to_string()))?;
                compact.push(serde_json::to_string(&value).unwrap_or_default());
                python.push(stringify_python_style(&value));
            }
            Ok(json!({"compact": compact, "python": python}))
        })(),
        other => Err(ConversionError::bad_request(format!("unknown op: {other}"))),
    };
    match outcome {
        Ok(result) => {
            let mut value = success(result);
            value["name"] = json!(case.name);
            value
        }
        Err(error) => {
            let mut value = failure(error);
            value["name"] = json!(case.name);
            value
        }
    }
}

#[tokio::main]
async fn main() {
    let stdin = io::stdin();
    let stdout = io::stdout();
    let mut out = io::BufWriter::new(stdout.lock());
    for line in stdin.lock().lines() {
        let line = match line {
            Ok(line) => line,
            Err(error) => {
                eprintln!("read error: {error}");
                break;
            }
        };
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with("//") {
            continue;
        }
        let result = match serde_json::from_str::<Case>(trimmed) {
            Ok(case) => handle(case).await,
            Err(error) => json!({"ok": false, "error": {"kind": "case", "detail": error.to_string()}}),
        };
        let _ = writeln!(out, "{}", serde_json::to_string(&result).unwrap_or_default());
    }
}
// ---------------------------------------------------------------- images

/// Stub fetcher: deterministic bytes derived from the URL, with two failure
/// modes so retry behaviour is exercised.
struct RefgenFetcher;

fn first_attempt(url: &str) -> bool {
    use std::collections::HashSet;
    use std::sync::{Mutex, OnceLock};

    static SEEN: OnceLock<Mutex<HashSet<String>>> = OnceLock::new();
    let seen = SEEN.get_or_init(|| Mutex::new(HashSet::new()));
    let mut guard = seen.lock().expect("fetch state lock");
    guard.insert(url.to_string())
}

impl ImageFetcher for RefgenFetcher {
    async fn fetch(&self, url: &str, budget: &ImageByteBudget) -> Result<Vec<u8>, ImageError> {
        if url.contains("fail-always") {
            return Err(ImageError::Fetch {
                url: url.to_string(),
                source: Box::new(std::io::Error::other("stub failure")),
            });
        }
        if url.contains("fail-once") && first_attempt(url) {
            return Err(ImageError::Fetch {
                url: url.to_string(),
                source: Box::new(std::io::Error::other("stub transient failure")),
            });
        }
        let data = format!("bytes:{url}").into_bytes();
        budget.reserve(data.len())?;
        Ok(data)
    }
}

/// Stub preprocessor: deterministic dimensions and an option fingerprint.
struct RefgenPreprocessor;

impl ImagePreprocessor for RefgenPreprocessor {
    async fn preprocess(
        &self,
        data: Vec<u8>,
        options: PreprocessOptions,
    ) -> Result<ImageInfo, ImageError> {
        let len = data.len();
        let mut out = data;
        out.extend_from_slice(
            format!(
                "|{}|{}|{}",
                detail_name(options.detail),
                options.max_dimension_px,
                options.low_detail_max_dimension_px
            )
            .as_bytes(),
        );
        Ok(ImageInfo {
            data: out,
            width: (len % 97 + 1) as u32,
            height: (len / 97 + 1) as u32,
        })
    }
}

fn hex_encode(data: &[u8]) -> String {
    let mut out = String::with_capacity(data.len() * 2);
    for byte in data {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

fn parse_detail(name: &Option<String>) -> ImageDetail {
    match name.as_deref() {
        Some("low") => ImageDetail::Low,
        Some("original") => ImageDetail::Original,
        Some("auto") => ImageDetail::Auto,
        _ => ImageDetail::High,
    }
}

fn parse_image_sources(specs: &[SourceSpec]) -> Vec<ImageSource> {
    specs
        .iter()
        .map(|spec| match spec {
            SourceSpec::DataUrl { data_url, detail } => ImageSource::DataUrl {
                data_url: data_url.clone(),
                detail: parse_detail(detail),
            },
            SourceSpec::Url { url, detail } => ImageSource::Url {
                url: url.clone(),
                detail: parse_detail(detail),
            },
            SourceSpec::Bytes { hex, detail } => ImageSource::Bytes {
                data: hex_decode(hex),
                detail: parse_detail(detail),
            },
        })
        .collect()
}

fn hex_decode(text: &str) -> Vec<u8> {
    let mut out = Vec::with_capacity(text.len() / 2);
    let bytes = text.as_bytes();
    let mut index = 0;
    while index + 1 < bytes.len() {
        let high = (bytes[index] as char).to_digit(16).unwrap_or(0) as u8;
        let low = (bytes[index + 1] as char).to_digit(16).unwrap_or(0) as u8;
        out.push(high * 16 + low);
        index += 2;
    }
    out
}

async fn run_image(case: &Case) -> Result<Value, ConversionError> {
    let sources = parse_image_sources(&case.sources.clone().unwrap_or_default());
    let mut quota = ImageQuota::new();
    let resolver = ImageResolver::new(RefgenFetcher, RefgenPreprocessor)
        .with_retry(RetryPolicy::new(vec![Duration::ZERO, Duration::ZERO]));
    match resolver.resolve(&sources, &mut quota).await {
        Ok(data) => Ok(json!({
            "images": data
                .images
                .iter()
                .map(|image| json!({
                    "width": image.width,
                    "height": image.height,
                    "data": hex_encode(&image.data),
                }))
                .collect::<Vec<_>>(),
            "quota_images": quota.image_count(),
            "quota_bytes": quota.byte_size(),
        })),
        Err(error) => Err(ConversionError::bad_request(error.to_string())),
    }
}