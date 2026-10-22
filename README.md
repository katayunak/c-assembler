# ctxasm

Builds one LLM input from six context layers, always in this order:

1. `system_instructions`: rules, role, safety
2. `state_and_memory`: user profile, past decisions, task state
3. `retrieved_knowledge`: doc chunks, search results
4. `tool_definitions`: tools and their schemas
5. `conversation_history`: past turns, oldest first
6. `current_user_input`: the request, always last

If the input is too big, it drops low-value items until it fits. It also writes a manifest: tokens per layer, budget, status, and what was kept or dropped and why.

## Status: milestone 2

- Done: check input, layer order, escape fake tags, count tokens, trim to budget, per-layer caps, overflow status.
- Not yet: compression, Claude API format, exact token counts, Python version, evals.

## Use

```bash
go test ./...
```

```bash
go run ./cmd/ctxasm assemble -c examples/config.json -i examples/input.json -m manifest.json
```

```bash
go run ./cmd/ctxasm inspect -c examples/config-small.json -i examples/input.json
```

From Go:

```go
p, m, err := asm.Assemble(input, config, nil) // nil = built-in estimator
if errors.Is(err, asm.ErrOverflow) {
	// no payload; m says why
}
fmt.Println(p.Text(), m.Status, m.TotalTokens)
```

## Input

See [examples/input.json](examples/input.json).

- Layers 1–3 are lists of items: `id`, `text`, optional `source` and `critical`.
- Tools: `name`, `description`, `input_schema`, optional `critical`.
- History turns: `id`, `role` (`user` or `assistant`), `text`, optional `critical`.
- User: `text`, optional `params` and `attachments`. The text is placed last.

Empty layers are kept (as empty tags) and listed under `missing`. Only an empty user request is an error.

## Config

| field | meaning |
|---|---|
| `version` | must be `"1"` |
| `model` | target model |
| `context_window` | model context size in tokens |
| `max_input` | extra cap on input; 0 = none |
| `reserved_output` | tokens kept for the answer |
| `safety_margin` | spare tokens for count errors |
| `chars_per_token` | estimator ratio; default 3.5 |
| `layer_budgets` | optional cap per layer, e.g. `{"retrieved_knowledge": 5000}`. Tags count too. |
| `keep_turns` | last turns that are never dropped; default 2 |

`available_input = context_window - reserved_output - safety_margin`, capped by `max_input`.

## Trimming

If the input fits, nothing changes (`status: ok`). If not, items are dropped in this order until it fits (`status: trimmed`):

1. **Duplicates**: same text, ignoring case and spaces. The kept copy lists both sources.
2. **Knowledge**: lowest rank first. Rank = number of useful words shared with the request.
3. **History**: oldest first, but never the last `keep_turns`.
4. **Tools**: lowest rank first.

Layer caps are checked first, then the total.

Never dropped: system instructions, other memory, `critical` items, and the user request. If it still doesn't fit, `status` is `overflow`, no payload is returned, and the CLI exits with code 1.

## Output

- Fits: [payload.txt](examples/payload.txt), [manifest.json](examples/manifest.json)
- Trimmed: [payload-small.txt](examples/payload-small.txt), [manifest-small.json](examples/manifest-small.json)

The manifest holds:
- the model, the tokenizer and `estimated`;
- tokens per layer, the total and the budget;
- `status`;
- for each item: id, source, tokens, `action` (`included`/`dropped`) and `reason`;
- missing layers, warnings, and input and config hashes.

It never holds item text.

## Tests

All tests run offline with `go test ./...`. They include:
- unit tests for each rule;
- a golden test (the `examples/` files must match the output);
- a budget sweep (every budget from 1 token to full size);
- a fuzz test for tag escaping;
- CLI tests for output and exit codes.

After an intended output change, refresh the examples:

```bash
go test ./asm -update
```

Search for escaping bugs with random input:

```bash
go test ./asm -run '^$' -fuzz=FuzzEscape -fuzztime=30s
```

## Safety

Text that looks like a layer tag (such as `</retrieved_knowledge>`) becomes `&lt;/retrieved_knowledge>`, so content can't break out of its layer. Nothing else in the text changes.

## Limits

- Token counts are estimates (chars ÷ 3.5) and can be off by 20% or more. Set `safety_margin` to cover this.
- Ranking is simple word overlap. It misses synonyms and favors long chunks.
- Only exact duplicates are found, not near ones.
- Items are dropped whole. Nothing is shortened yet.
- Output is plain tagged text. Tools are written as text, not as Claude API tool objects.
- Layer order alone does not fix lost-in-the-middle. Evals come later.

## Next

- M3: Claude message format, `count_tokens`, tuned estimator.
- M4: compression.
- M5: Python reference, eval set, lost-in-the-middle report.
