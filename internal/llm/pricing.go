package llm

// Pricing constants for supported Anthropic models. Values are USD per
// 1,000,000 tokens, matching the units Anthropic publishes. Update
// these in a focused PR when Anthropic revises pricing; ADR-017 flags
// this as a known follow-up.
//
// Current snapshot (2026-05-05):
//
//	claude-sonnet-4-6 :  $3 / 1M input,  $15 / 1M output
//	claude-opus-4-7   : $15 / 1M input,  $75 / 1M output
const (
	// SonnetInputUSDPerMTok is the input price for ModelSonnet in USD
	// per 1,000,000 tokens.
	SonnetInputUSDPerMTok = 3.0
	// SonnetOutputUSDPerMTok is the output price for ModelSonnet in
	// USD per 1,000,000 tokens.
	SonnetOutputUSDPerMTok = 15.0
	// OpusInputUSDPerMTok is the input price for ModelOpus in USD per
	// 1,000,000 tokens.
	OpusInputUSDPerMTok = 15.0
	// OpusOutputUSDPerMTok is the output price for ModelOpus in USD
	// per 1,000,000 tokens.
	OpusOutputUSDPerMTok = 75.0

	// tokensPerMillion is the divisor that converts a token count into
	// units of "millions of tokens" so price-per-Mtok constants line up.
	tokensPerMillion = 1_000_000.0
)

// Cost returns the dollar cost of a single LLM call given the model
// identifier and the input/output token counts the provider reports.
//
// Unknown models return 0; the orchestrator should treat 0 as "cost
// not tracked" rather than "free" — Stage 7 logs an explicit warning
// when this happens so an unknown model identifier doesn't silently
// suppress observability. Negative token counts are clamped to zero
// rather than panicking; a provider that reports negatives is buggy
// upstream and we'd rather still emit a usage record.
func Cost(model string, inputTokens, outputTokens int64) float64 {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	in, out, ok := pricesFor(model)
	if !ok {
		return 0
	}
	return (float64(inputTokens)*in + float64(outputTokens)*out) / tokensPerMillion
}

// pricesFor returns (input, output) USD-per-1M-token prices for model.
// The third return value is false when the model is not recognised.
func pricesFor(model string) (in, out float64, ok bool) {
	switch model {
	case ModelSonnet:
		return SonnetInputUSDPerMTok, SonnetOutputUSDPerMTok, true
	case ModelOpus:
		return OpusInputUSDPerMTok, OpusOutputUSDPerMTok, true
	default:
		return 0, 0, false
	}
}
