package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stargate-backend/storage/auth"
)

const (
	chatRoom      = "contract_chat_verified"
	chatKey       = "chat-key"
	chatKeyWallet = "tb1qchatverifiedwallet"
)

func newChatTestServer() *HTTPMCPServer {
	keys := &multiKeyWalletValidator{wallets: map[string]string{chatKey: chatKeyWallet}}
	return NewHTTPMCPServer(nil, keys, nil, nil, nil, nil, auth.NewChallengeStore(10*time.Minute))
}

func postChatSend(t *testing.T, s *HTTPMCPServer, apiKey string, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/mcp/chat/send", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		r.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	s.handleChatSend(w, r)
	return w
}

func callChatSendTool(t *testing.T, s *HTTPMCPServer, apiKey string) MCPResponse {
	t.Helper()
	raw, _ := json.Marshal(MCPRequest{Tool: "chat_send", Arguments: map[string]interface{}{
		"room_id": chatRoom, "agent_id": "creator", "content": "via tool",
	}})
	r := httptest.NewRequest("POST", "/mcp/call", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		r.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	s.handleToolCall(w, r)
	var resp MCPResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode tool response: %v: %s", err, w.Body.String())
	}
	return resp
}

func lastChatMessage(t *testing.T, s *HTTPMCPServer) *ChatMessage {
	t.Helper()
	msgs := s.chatHub.GetRecentMessages(chatRoom, 1)
	if len(msgs) != 1 {
		t.Fatalf("expected a stored message, got %d", len(msgs))
	}
	return msgs[0]
}

func TestChatSendLabelsSender(t *testing.T) {
	t.Run("anonymous send stays allowed and unverified", func(t *testing.T) {
		s := newChatTestServer()
		w := postChatSend(t, s, "", map[string]interface{}{"room_id": chatRoom, "agent_id": "creator", "content": "hi"})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if msg := lastChatMessage(t, s); msg.Verified || msg.Wallet != "" {
			t.Fatalf("anonymous message must be unverified, got verified=%v wallet=%q", msg.Verified, msg.Wallet)
		}
	})

	t.Run("body cannot forge the verified label", func(t *testing.T) {
		s := newChatTestServer()
		w := postChatSend(t, s, "", map[string]interface{}{
			"room_id": chatRoom, "agent_id": "creator", "content": "trust me",
			"verified": true, "wallet": chatKeyWallet,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if msg := lastChatMessage(t, s); msg.Verified || msg.Wallet != "" {
			t.Fatalf("forged fields must be ignored, got verified=%v wallet=%q", msg.Verified, msg.Wallet)
		}
	})

	t.Run("valid key labels the message with its wallet", func(t *testing.T) {
		s := newChatTestServer()
		w := postChatSend(t, s, chatKey, map[string]interface{}{"room_id": chatRoom, "agent_id": "creator", "content": "hi"})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp ChatSendResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if !resp.Verified {
			t.Fatalf("expected verified response, got %s", w.Body.String())
		}
		if msg := lastChatMessage(t, s); !msg.Verified || msg.Wallet != chatKeyWallet {
			t.Fatalf("expected verified message for %s, got verified=%v wallet=%q", chatKeyWallet, msg.Verified, msg.Wallet)
		}
	})

	t.Run("invalid key is rejected rather than downgraded", func(t *testing.T) {
		s := newChatTestServer()
		w := postChatSend(t, s, "wrong-key", map[string]interface{}{"room_id": chatRoom, "agent_id": "creator", "content": "hi"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
		}
		if msgs := s.chatHub.GetRecentMessages(chatRoom, 0); len(msgs) != 0 {
			t.Fatalf("rejected send must not reach the room, got %d messages", len(msgs))
		}
	})
}

func TestChatSendToolLabelsSender(t *testing.T) {
	t.Run("valid key", func(t *testing.T) {
		s := newChatTestServer()
		if resp := callChatSendTool(t, s, chatKey); !resp.Success {
			t.Fatalf("expected success, got %+v", resp)
		}
		if msg := lastChatMessage(t, s); !msg.Verified || msg.Wallet != chatKeyWallet {
			t.Fatalf("expected verified message, got verified=%v wallet=%q", msg.Verified, msg.Wallet)
		}
	})

	t.Run("anonymous", func(t *testing.T) {
		s := newChatTestServer()
		if resp := callChatSendTool(t, s, ""); !resp.Success {
			t.Fatalf("expected success, got %+v", resp)
		}
		if msg := lastChatMessage(t, s); msg.Verified {
			t.Fatalf("anonymous tool send must be unverified")
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		s := newChatTestServer()
		resp := callChatSendTool(t, s, "wrong-key")
		if resp.Success || resp.ErrorCode != ErrCodeUnauthorized {
			t.Fatalf("expected UNAUTHORIZED, got %+v", resp)
		}
		if msgs := s.chatHub.GetRecentMessages(chatRoom, 0); len(msgs) != 0 {
			t.Fatalf("rejected send must not reach the room, got %d messages", len(msgs))
		}
	})
}
