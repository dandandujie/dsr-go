#!/usr/bin/env python3
"""Generate the golden tokenizer corpus for the Go tests.

Run with the oracle venv from the repository root:

    /tmp/toktest-venv/bin/python encoding/tokenizer/testdata/gen_cases.py

It writes tokenizer_cases.json (V4.1) and tokenizer_cases_v4.json (V4) with the
exact output of tokenizers==0.23.2 for every case:

    ids                  = tok.encode(text, add_special_tokens=False).ids
    decoded              = tok.decode(ids, skip_special_tokens=False)
    decoded_skip_special = tok.decode(ids, skip_special_tokens=True)
"""

import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", ".."))

CASES = [
    # --- plain ASCII -------------------------------------------------------
    "Hello",
    "Hello, world!",
    "The quick brown fox jumps over the lazy dog.",
    "I'm sure it's fine; they're not, and we've all heard it.",
    "MixedCASE Words and lowercase letters.",
    "A",
    "a",
    "supercalifragilisticexpialidocious",
    "antidisestablishmentarianism",
    "Hello world, this is a plain ASCII sentence with several words in it.",
    "The rain in Spain stays mainly in the plain.",
    "one two three four five six seven eight nine ten",

    # --- Chinese -----------------------------------------------------------
    "中文测试",
    "你好，世界！",
    "这是一个用于测试分词器的中文句子。",
    "繁體字測試",
    "中文mixed英文",
    "汉字123数字",
    "「引用」和（括号）",
    "上海自来水来自海上",
    "深度学习模型的分词器",

    # --- Japanese ----------------------------------------------------------
    "日本語のテストです。",
    "こんにちは、世界！",
    "カタカナとひらがなと漢字",
    "ｶﾀｶﾅﾊﾝｶｸ",
    "ｆｕｌｌｗｉｄｔｈ ｔｅｘｔ",
    "東京タワーは333mです",

    # --- Korean ------------------------------------------------------------
    "한국어 테스트입니다.",
    "안녕하세요 세계",
    "한글과 English 혼합 문장",
    "서울특별시",

    # --- emoji and astral characters --------------------------------------
    "😀",
    "😀🎉🚀",
    "Hello 😀 world",
    "👍🏽",
    "👨‍👩‍👧‍👦",
    "𝕳𝖊𝖑𝖑𝖔 𝖂𝖔𝖗𝖑𝖉",
    "𐍈𐍉 𐌰𐌱",
    "🎌🇯🇵",
    "☺️",
    "中😀文",
    "🚀 launch 🚀",
    "emoji at the end 😅",

    # --- digit runs --------------------------------------------------------
    "1",
    "12",
    "123",
    "1234",
    "12345",
    "1234567890",
    "007",
    "3.14159",
    "1,000,000",
    "a1b2c3",
    "٢٠٢٤",
    "½ Ⅷ ⅓",
    "12 345 6789",
    "v1.2.3-beta.4",
    "第1章第2节",

    # --- punctuation runs --------------------------------------------------
    "!!!",
    "?!?!",
    "...",
    "—",
    "()[]{}",
    "a(b)c",
    "<html></html>",
    "https://example.com/path?q=1&r=2#frag",
    "user@example.com",
    "#hashtag @mention",
    "C++ & C#",
    "a+b=c*d/e",
    "100% sure!",
    "~~strike~~ **bold** __under__",

    # --- source code -------------------------------------------------------
    "\x60\x60\x60go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n\x60\x60\x60",
    "def f(x):\n    return x + 1",
    "const s = \x60template ${x}\x60;",
    "SELECT * FROM t WHERE a = 'b';",
    "if (a && b) { c = d ?? e; }",
    "{\"key\": \"value\", \"n\": 1, \"ok\": true, \"nil\": null}",
    "{\n  \"messages\": [\n    {\"role\": \"user\", \"content\": \"hi\"},\n    {\"role\": \"assistant\", \"content\": \"hello\"}\n  ],\n  \"temperature\": 0.7\n}",
    "let x = [1, 2, 3].map(v => v * 2);",
    "ls -la | grep foo > out.txt 2>&1",
    "#[derive(Debug)]\nstruct Foo { a: u32 }",
    "s = '''triple''' + \"quoted\" + 'single'",
    "# Title\n\n- item 1\n- item 2\n\n**bold** and \x60code\x60\n\n> quote",
    "for (int i = 0; i < 10; i++) { sum += i; }",
    "Traceback (most recent call last):\n  File \"x.py\", line 3\nValueError: bad",
    "printf(\"%s\\n\", argv[0]);",
    "type Tokenizer struct {\n\tvocab map[string]uint32\n}",
    "git commit -m \"fix: handle \\\"quotes\\\" and $VARS\"",

    # --- whitespace --------------------------------------------------------
    "a  b",
    "a \n\n b",
    "trailing   ",
    "  leading",
    "   ",
    "\n\n",
    "\n",
    "\t",
    "\r",
    "\r\n",
    "a\rb",
    "a\r\nb",
    "a\t\tb",
    " \t \n \t ",
    "line1\nline2\nline3",
    "line1\n\nline2",
    "a\n b",
    "\u00a0",
    "a\u00a0b",
    "\u3000\u3000",
    "  \t\n  ",
    "\n ",
    " \n",
    "a  \n  b",
    "a\n \nb",
    "x\n \n\ny",
    "a \r\n b",
    "a\r \nb",
    "\r \n",
    "\v\f",
    "\x1c\x1d",
    "a\u2003b",
    "  a  b  ",
    "\n\n\n",
    "a\n\n\n\nb",
    "trailing\t",
    "trailing\n",
    "  ",

    # --- added / special tokens -------------------------------------------
    "<｜begin▁of▁sentence｜>",
    "<｜end▁of▁sentence｜>",
    "<｜▁pad▁｜>",
    "<｜User｜>",
    "<｜Assistant｜>",
    "<｜System｜>",
    "<｜latest_reminder｜>",
    "<｜image｜>",
    "<｜image2｜>",
    "<｜place▁holder▁no▁0｜>",
    "｜DSML｜",
    "<｜DSML｜ calls>",
    "<think>",
    "</think>",
    "<｜begin▁of▁sentence｜>Hello<｜end▁of▁sentence｜>",
    "Hello <｜image｜> world",
    "<｜image｜><｜image｜>",
    "<think>reasoning here</think>",
    "<｜User｜>hi<｜Assistant｜>",
    "x<｜User｜>y",
    "a<｜User｜>b<｜Assistant｜>c",
    "<|image|>",
    "<｜fim▁hole｜>",
    "<｜end▁of▁sentence｜><｜end▁of▁sentence｜>",
    "<｜Assistant｜> Hello!",
    "<｜DSML｜tool_calls>",
    "<｜DSML｜invoke name=\"get_weather\">",

    # --- added tokens that exist in only one of the two vocabularies -------
    # These are the tokens whose ids 128799 and 129265..129272 differ between
    # V4 and V4.1; they make the two golden files genuinely different.
    "<｜place▁holder▁no▁799｜>",  # V4 only
    "<｜/table>｜",
    "<｜table｜>",
    "<｜/td｜>",
    "<｜td｜>",
    "<｜/tr｜>",
    "<｜tr｜>",
    "<|place_holder_mm_span_0436|>",  # V4.1 only
    "<|place_holder_mm_span_0437|>",
    "<|place_holder_mm_span_0438|>",
    "<|place_holder_mm_span_0439|>",
    "<|place_holder_mm_span_0440|>",
    "<|place_holder_mm_span_0441|>",
    "<|place_holder_mm_span_0442|>",
    "<｜table｜><｜tr｜><｜td｜>cell<｜/td｜><｜/tr｜><｜/table｜>",
    "cell <|place_holder_mm_span_0439|> end",

    # --- tool-call markup --------------------------------------------------
    "<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"get_weather\">\n<｜DSML｜ parameter name=\"location\" string=\"true\">Paris</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>",
    "<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"get_weather\">\n<｜DSML｜parameter name=\"location\" string=\"true\">Paris</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>",
    "<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"search\">\n<｜DSML｜ parameter name=\"query\" string=\"true\">Go tokenizer</｜DSML｜ parameter>\n<｜DSML｜ parameter name=\"limit\" string=\"false\">10</｜DSML｜ parameter>\n<｜DSML｜ parameter name=\"filters\" string=\"false\">{\"lang\": \"en\", \"recent\": true}</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>",
    "I will call a tool.\n<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"noop\">\n</｜DSML｜ invoke>\n</｜DSML｜ calls>",
    "<think>\nThe user wants the weather.\n</think>\n<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"get_weather\">\n<｜DSML｜ parameter name=\"location\" string=\"true\">上海</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>",
    "Tool result: {\"temperature\": 21, \"unit\": \"C\"}",

    # --- JSON snippets -----------------------------------------------------
    "{\"a\": [1, 2, {\"b\": null}], \"c\": \"d\\ne\"}",
    "[{\"id\": 1}, {\"id\": 2}]",
    "{\"text\": \"中文\", \"emoji\": \"😀\"}",
    "{\"nested\": {\"deep\": {\"deeper\": [true, false]}}}",

    # --- long mixed text ---------------------------------------------------
    "You are a helpful assistant. 你是一个有用的助手。\n\n"
    "Here is a code block:\n"
    "\x60\x60\x60python\n"
    "def greet(name: str) -> str:\n"
    "    return f\"Hello, {name}!\"\n"
    "\x60\x60\x60\n"
    "And some numbers: 1, 12, 123, 1234, 12345, 1234567890.\n"
    "Emoji: 😀🎉🚀 and CJK: 日本語のテスト、한국어 테스트.\n"
    "Trailing spaces   \nand a tab\there.\n",

    "## Tools\n\n"
    "You have access to a set of tools to help answer the user's question. "
    "You can invoke tools by writing a \"<｜DSML｜ calls>\" block like the following:\n\n"
    "<｜DSML｜ calls>\n"
    "<｜DSML｜ invoke name=\"$TOOL_NAME\">\n"
    "<｜DSML｜ parameter name=\"$PARAMETER_NAME\" string=\"true|false\">$PARAMETER_VALUE</｜DSML｜ parameter>\n"
    "</｜DSML｜ invoke>\n"
    "</｜DSML｜ calls>\n\n"
    "String parameters should be specified as is and set \x60string=\"true\"\x60. For all other types "
    "(numbers, booleans, arrays, objects), pass the value in JSON format and set \x60string=\"false\"\x60.\n",

    "The quick brown fox jumps over the lazy dog. 1234567890 !!! "
    "中文测试，日本語のテスト，한국어 테스트. 😀🎉 "
    "{\"key\": \"value\", \"list\": [1, 2, 3]} "
    "https://example.com/a/b?c=d&e=f#g "
    "trailing   \n\tmixed\twhitespace\n\n  and more.",

    "".join(
        [
            "Line %d: " % i
            + ("中文" if i % 3 == 0 else "ascii")
            + (" 😀" if i % 5 == 0 else "")
            + (" 12,345" if i % 7 == 0 else "")
            + "\n"
            for i in range(40)
        ]
    ),

    "def tokenize(text):\n"
    "    ids = []\n"
    "    for token in text.split():\n"
    "        ids.extend(encode(token))\n"
    "    return ids\n"
    "\n"
    "# 混合注释 with emoji 😀 and numbers 42\n"
    "result = tokenize(\"Hello 世界\")\n"
    "print(f\"{len(result)=}\")\n",

    "<｜begin▁of▁sentence｜>You are a helpful assistant.<｜end▁of▁sentence｜>"
    "<｜begin▁of▁sentence｜>Hello!<｜end▁of▁sentence｜>",
]


def random_cases(count=150, seed=20240910):
    """Deterministic pseudo-random cases that mix scripts, whitespace and
    added-token fragments; the seed keeps the golden files reproducible."""
    import random

    rng = random.Random(seed)
    alphabet = list(
        "abcXYZ019 "
        "!\"#$%&'()*+,-./:;<=>?@[\\]^_\x60{|}~"
        "\t\n\r\v\f"
        "\u00a0\u1680\u2000\u2003\u2028\u2029\u202f\u205f\u3000\u0085"
        "\x00\x01\x07\x1c\x1f\x7f"
        "中文汉字测试繁體日本"
        "ひらがなカタカナｶﾀｶﾅ"
        "한국어테스트"
        "😀🎉🚀👍🏽☺️"
        "𝕳𝖊𝖑𝖑𝖔𐍈𐌰"
        "\u0301\u0308\u200d\u200b\u200e\u202e"
        "½Ⅷ٣٤"
        "｜<｜｜><think>"
    )
    fragments = [
        "<｜DSML｜ calls>",
        "<｜User｜>",
        "<｜image｜>",
        "<｜begin▁of▁sentence｜>",
        "<think>",
        "</think>",
        "<｜place▁holder▁no▁0｜>",
        "｜DSML｜",
        "</｜DSML｜ invoke>",
        "<｜latest_reminder｜>",
        "▁",
        "Ġ",
    ]
    cases = []
    while len(cases) < count:
        parts = []
        for _ in range(rng.randint(1, 30)):
            if rng.random() < 0.10:
                parts.append(rng.choice(fragments))
            else:
                parts.append(rng.choice(alphabet))
        text = "".join(parts)
        if not text.strip() and rng.random() < 0.7:
            continue  # keep all-whitespace cases rare but present
        cases.append(text)
    return cases


CASES += random_cases()


def build(oracle, name, path):
    cases = []
    for text in CASES:
        ids = oracle.encode(text, add_special_tokens=False).ids
        cases.append(
            {
                "text": text,
                "ids": ids,
                "decoded": oracle.decode(ids, skip_special_tokens=False),
                "decoded_skip_special": oracle.decode(ids, skip_special_tokens=True),
            }
        )
    document = {"tokenizer": name, "cases": cases}
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(document, fh, ensure_ascii=False, indent=1)
        fh.write("\n")
    print("wrote %s: %d cases" % (path, len(cases)))


def main():
    from tokenizers import Tokenizer

    for name, filename in (("v41", "tokenizer_cases.json"), ("v4", "tokenizer_cases_v4.json")):
        path = os.path.join(ROOT, "static", "tokenizers", name, "tokenizer.json")
        if not os.path.exists(path):
            print("skipping %s: %s not found" % (name, path), file=sys.stderr)
            continue
        build(Tokenizer.from_file(path), name, os.path.join(HERE, filename))


if __name__ == "__main__":
    main()
