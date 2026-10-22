# ctxasm

Builds one LLM input from six context layers, always in this order:

1. `system_instructions`: rules, role, safety
2. `state_and_memory`: user profile, past decisions, task state
3. `retrieved_knowledge`: doc chunks, search results
4. `tool_definitions`: tools and their schemas
5. `conversation_history`: past turns, oldest first
6. `current_user_input`: the request, always last

It also writes a manifest: tokens per layer, total, budget and which items went in.

## Status: milestone 1

- Done: check input, put layers in order, escape fake tags, count tokens, flag over budget.
- Not yet: trimming, ranking, dedup, compression, Claude API format, exact token counts, Python version, evals.

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
fmt.Println(p.Text(), m.TotalTokens, m.OverBudget)
```

## Input

See [examples/input.json](examples/input.json).

- Layers 1–3 are lists of items: `id`, `text`, optional `source` and `critical`.
- Tools: `name`, `description`, `input_schema`.
- History turns: `id`, `role` (`user` or `assistant`), `text`.
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

`available_input = context_window - reserved_output - safety_margin`, capped by `max_input`.

## Output

- Payload: [examples/payload.txt](examples/payload.txt)
- Manifest: [examples/manifest.json](examples/manifest.json). It holds the model, tokenizer, `estimated`, tokens per layer, total, budget, `over_budget`, item ids, sources and tokens, missing layers, warnings, and input and config hashes. It never holds item text.

## Safety

Text that looks like a layer tag (such as `</retrieved_knowledge>`) becomes `&lt;/retrieved_knowledge>`, so content can't break out of its layer. Nothing else in the text changes.

## Limits

- Token counts are estimates (chars ÷ 3.5) and can be off by 20% or more. Set `safety_margin` to cover this.
- Going over budget is only flagged, not fixed.
- Output is plain tagged text. Tools are written as text, not as Claude API tool objects.
- Layer order alone does not fix lost-in-the-middle. Evals come later.

## Next

- M2: budget. Rank, dedup and drop low-value items; never drop critical ones; report overflow.
- M3: Claude message format, `count_tokens`, tuned estimator.
- M4: Python reference, eval set, lost-in-the-middle report.
