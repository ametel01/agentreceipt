# AgentReceipt

AgentReceipt records local evidence from AI coding sessions and turns it into reviewable, verifiable receipt artifacts.

## Language

**Provider Evidence**:
Normalized observations from an AI coding provider, such as tool calls, command attempts, command results, token usage, and provider parse warnings. Provider Evidence enriches the receipt but does not replace Git or filesystem evidence.
_Avoid_: Provider payloads, provider maps, provider API data
