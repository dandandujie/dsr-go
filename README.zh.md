<div align="center">
  <img src="static/deepseek-whale.svg" alt="DeepSeek" width="240">
</div>

# dsr-go

[English](README.md) | **中文**

dsr-go 是 [deepseek-recipe](https://github.com/deepseek-ai/deepseek-recipe)
的纯 Go 重写版本。它把不同格式的 API 请求统一转换为 Conversation 格式，编码为
DeepSeek 模型的 prompt，并把模型输出转换回对应格式的响应。可以用这些包把推理后端
接入支持多种 API 格式的服务；模型推理、工具执行和 HTTP 传输由调用方提供。

本项目与原 Rust 实现做了逐字节对比验证：相同输入会得到完全一致的 prompt、token ID、
流式事件和响应（见[差分测试](#差分测试)）。

## 支持范围

- **请求/响应格式**：Messages、Chat Completions、Responses 请求的转换，流式响应和
  完整响应。支持文本、图片、思考内容和客户端工具调用。
- **Prompt**：把 DeepSeek V4 / V4.1 会话编码为 prompt 或 token ID，内置纯 Go 实现的
  HuggingFace BPE tokenizer。
- **生成参数**：思考模式、reasoning effort、`temperature`、`top_p` 和输出 token 上限。
- **输出解析**：思考内容、工具调用、JSON 对象输出和停止序列。
- **图片**：支持 base64 和外部 URL，DeepSeek V4.1 预处理使用纯 Go 实现（不依赖
  OpenCV，也不需要 cgo）。
- **工具定义**：函数工具；Responses API 还支持 tool namespace 和 `apply_patch`
  自定义工具。

## 尚未支持

与 Rust 版本保持一致：

- token 概率（`logprobs`、`top_logprobs`）。
- 文档内容、音视频输入，以及通过 `file_id` 获取文件。
- 服务端工具执行，例如 `web_search`。
- JSON Schema 与正则输出约束，以及工具 `strict` 设置的强制校验。
- 单次 Chat Completions 请求返回多个结果（`n > 1`）。
- `apply_patch` 之外的 Responses 自定义工具定义。
- Responses 会话存储，以及通过 `previous_response_id` 获取上下文。
- Responses 加密思考内容（`encrypted_content`）。

与原项目的差异：

- 不移植 PyO3 Python 绑定；请直接使用 Go 包或示例 HTTP 服务。
- 图片预处理改用纯 Go（标准库 `image`、`golang.org/x/image` 以及 WebP 编码器），
  不再依赖 OpenCV，构建无需 cgo 和本地依赖。
- tokenizer 在本仓库内实现，直接读取仓库自带的 `tokenizer.json`。

## 安装

```sh
go get github.com/dandandujie/dsr-go
```

本模块没有 cgo 依赖，仅需 `golang.org/x/image`（图片解码）和
`github.com/santhosh-tekuri/jsonschema/v6`（工具参数 schema 校验）。

## 使用方式

把 Chat Completions 请求转换为 DeepSeek V4.1 prompt，完整示例见
[`examples/quickstart`](examples/quickstart/main.go)：

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/dandandujie/dsr-go/encoding/v4/dsv41"
	"github.com/dandandujie/dsr-go/recipe/openai/chatcompletion"
	"github.com/dandandujie/dsr-go/recipe/request"
)

func main() {
	body := []byte(`{
		"model": "deepseek-flash",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)

	var typed chatcompletion.ChatCompletionRequest
	if err := json.Unmarshal(body, &typed); err != nil {
		log.Fatal(err)
	}
	converted, err := typed.Convert(request.NewConversionOptions())
	if err != nil {
		log.Fatal(err)
	}
	rendered := dsv41.New().RenderConversation(converted.Conversation)
	fmt.Println(rendered.Prompt)
}
```

在仓库根目录运行：

```sh
go run ./examples/quickstart
```

### 流式响应

`StreamProcessor` 把后端输出转换为 Messages、Chat Completions 或 Responses 事件。
参见 [`examples/streaming`](examples/streaming/main.go) 和
[流式响应指南](docs/streaming.md)。

### 使用 tokenizer 编解码

给 encoding 附加 tokenizer 可以得到 token ID；给 stream processor 附加同一个
tokenizer 可以解码后端返回的 token ID。参见
[`examples/tokenizer`](examples/tokenizer/main.go) 和
[tokenizer 指南](docs/tokenizer.md)。

### 编解码演示页

```sh
go run ./cmd/demo
```

打开 <http://127.0.0.1:7778>。可用 `DEMO_ADDR` 修改监听地址。

### 示例服务

```sh
go run ./cmd/server
```

服务监听 <http://127.0.0.1:7777>，提供 `POST /v1/chat/completions`、
`POST /v1/responses`、`POST /v1/messages` 三个接口（使用模拟推理）。可用
`SERVER_ADDR` 修改监听地址。

## 包结构

| 包 | 说明 |
| --- | --- |
| [`core`](core) | 共享的会话、消息、图片和工具类型。 |
| [`core/jsonx`](core/jsonx) | 与 `serde_json` 一致的有序 JSON 值模型。 |
| [`encoding`](encoding) | prompt 渲染与 token 编码。 |
| [`encoding/tokenizer`](encoding/tokenizer) | 纯 Go 的 HuggingFace BPE tokenizer。 |
| [`encoding/v4/dsv4`](encoding/v4/dsv4)、[`encoding/v4/dsv41`](encoding/v4/dsv41) | DeepSeek V4 / V4.1 prompt 渲染。 |
| [`image`](image) | 图片获取与 V4.1 预处理。 |
| [`recipe`](recipe) | 协议转换与模型输出解析。 |
| [`recipe/openai/chatcompletion`](recipe/openai/chatcompletion) | Chat Completions 适配器。 |
| [`recipe/openai/responses`](recipe/openai/responses) | Responses 适配器。 |
| [`recipe/anthropic/messages`](recipe/anthropic/messages) | Messages 适配器。 |
| [`cmd/server`](cmd/server) | 使用模拟推理的 HTTP 服务示例。 |
| [`cmd/demo`](cmd/demo) | 编解码演示页面。 |

## 差分测试

`testdata/corpus.jsonl` 是测试语料，`testdata/goldens/rust.jsonl` 是原 Rust
实现产出的参考结果。`golden/` 会用 Go 实现重放每个用例，要求结果逐字节一致
（时间戳会被归一化）。

```sh
go test ./...
```

参考结果生成器位于 [`tools/refgen`](tools/refgen)，其中的 README 说明了如何针对
Rust 仓库重新生成 golden 文件。

## 开发

参见[开发指南](docs/development.md)。

## 致谢

dsr-go 是 [deepseek-recipe](https://github.com/deepseek-ai/deepseek-recipe)
的独立 Go 移植版本：差分测试的参考结果由该项目生成，随仓库分发的 tokenizer 文件
也来自该项目。详见 [NOTICE](NOTICE)。

## 许可

项目代码与公开文档采用 [MIT 许可](LICENSE)。随仓库分发的 tokenizer 声明见
[static/tokenizers/README.md](static/tokenizers/README.md)。
