package tools

import (
	"fmt"
	"strings"
)

// formatAuthError returns an agent-friendly error message. When the error indicates
// authentication is required, it appends explicit guidance so the agent knows what to do.
func formatAuthError(operation string, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// Ownership/authorization errors must be caught BEFORE the auth-required
	// check: the upstream "Access denied — you don't own this agent" bubbles up
	// alongside an unrelated "got 401" from a fallback endpoint, and that 401
	// substring would otherwise misclassify it as an authentication failure —
	// sending the agent into a pointless re-auth loop. This is NOT fixable by
	// re-authenticating; the caller must target an agent the wallet owns.
	if isOwnershipError(msg) {
		return fmt.Sprintf(`ACCESS DENIED: this wallet does not own that agent (or it does not exist / is archived).

Error: %s

This is NOT an authentication problem — repeating the auth flow will not help. The agent chat and
control tools (send_agent_message, stop_agent, delete_agent, ...) work only on agents the
AUTHENTICATED wallet owns. Find an agent you own with list_agents or get_my_portfolio, then retry
with that agent_id and your own wallet_address.`, msg)
	}

	if !isAuthRequiredError(msg) {
		return fmt.Sprintf("%s failed: %v", operation, err)
	}

	guidance := getAuthGuidance(msg)
	return fmt.Sprintf(`AUTHENTICATION REQUIRED

Error: %s

GUIDANCE: %s

After completing the steps above, retry the original request.`, msg, guidance)
}

// isOwnershipError detects an upstream authorization/ownership rejection — the
// caller asked to act on an agent the authenticated wallet does not own. These
// must not be treated as authentication failures.
func isOwnershipError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "own this agent") || // "...you don't own this agent"
		strings.Contains(lower, "not owned by you") ||
		strings.Contains(lower, "access denied")
}

func isAuthRequiredError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "not authenticated") ||
		strings.Contains(lower, "orderly key not set") ||
		(strings.Contains(lower, "account registered") && strings.Contains(lower, "orderly key")) ||
		strings.Contains(lower, "401") ||
		strings.Contains(lower, "unauthorized")
}

func getAuthGuidance(msg string) string {
	lower := strings.ToLower(msg)

	// Need full flow: registration + orderly key
	if strings.Contains(lower, "not authenticated") || strings.Contains(lower, "prepare_registration") {
		return `To authenticate, perform these steps:
1. Call prepare_registration with the user's wallet_address.
2. If already_registered=true, skip to step 4.
3. Have the user sign the returned message_base64 with their Solana wallet and call complete_registration with the signature.
4. Call prepare_orderly_key with wallet_address.
5. Have the user sign the returned message_base64 and call complete_orderly_key with the signature.
6. Retry the original request.`
	}

	// Already registered, need orderly key only
	if strings.Contains(lower, "orderly key") || strings.Contains(lower, "complete_orderly_key") {
		return `To complete authentication, perform these steps:
1. Call prepare_orderly_key with the user's wallet_address.
2. Have the user sign the returned message_base64 with their Solana wallet.
3. Call complete_orderly_key with wallet_address and the signature.
4. Retry the original request.`
	}

	// Generic (e.g. 401 from API)
	return `Authentication may have expired or credentials are invalid. Perform the full auth flow:
1. Call prepare_registration with wallet_address. If already_registered, go to step 4.
2. User signs message_base64 → complete_registration.
3. Call prepare_orderly_key with wallet_address.
4. User signs message_base64 → complete_orderly_key.
5. Retry the original request.`
}
