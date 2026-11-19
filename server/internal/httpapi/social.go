package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yixiaozi/imgmesh/server/internal/store"
)

func (s *Server) likePhoto(w http.ResponseWriter, r *http.Request) {
	photoID, err := canonicalFingerprint(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if err := s.Store.SetLike(r.Context(), photoID, userID(r), r.Method == http.MethodPost); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) friendLists(w http.ResponseWriter, r *http.Request) {
	lists, err := s.Store.FriendLists(r.Context(), userID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"friends":  socialUsersJSON(lists.Friends),
		"incoming": socialUsersJSON(lists.Incoming),
		"outgoing": socialUsersJSON(lists.Outgoing),
	})
}

func (s *Server) requestFriend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"userId"`
	}
	if !readJSON(w, r, &body) || !validUserID(body.UserID) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	status, err := s.Store.RequestFriend(r.Context(), userID(r), body.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) acceptFriend(w http.ResponseWriter, r *http.Request) {
	other := chi.URLParam(r, "id")
	if !validUserID(other) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if err := s.Store.AcceptFriend(r.Context(), userID(r), other); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) declineFriend(w http.ResponseWriter, r *http.Request) {
	other := chi.URLParam(r, "id")
	if !validUserID(other) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if err := s.Store.DeclineFriend(r.Context(), userID(r), other); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeFriend(w http.ResponseWriter, r *http.Request) {
	other := chi.URLParam(r, "id")
	if !validUserID(other) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if err := s.Store.RemoveFriend(r.Context(), userID(r), other); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("with")
	if !validUserID(peer) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	var before *time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, raw)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		before = &parsed
	}
	msgs, err := s.Store.Messages(r.Context(), userID(r), peer, before)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, messageJSON(msg))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To   string `json:"to"`
		Body string `json:"body"`
	}
	if !readJSON(w, r, &body) || !validUserID(body.To) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	text, ok := normalizeMessage(body.Body)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	msg, err := s.Store.SendMessage(r.Context(), userID(r), body.To, text)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageJSON(msg))
}

func socialUsersJSON(people []store.SocialUser) []any {
	out := make([]any, 0, len(people))
	for _, person := range people {
		item := publicUserJSON(person.PublicUser)
		item["unread"] = person.Unread
		if person.LastBody != "" {
			item["lastBody"] = person.LastBody
			item["lastAt"] = person.LastAt
		}
		out = append(out, item)
	}
	return out
}

func messageJSON(msg store.DirectMessage) map[string]any {
	return map[string]any{
		"id":          msg.ID,
		"senderId":    msg.SenderID,
		"recipientId": msg.RecipientID,
		"body":        msg.Body,
		"createdAt":   msg.CreatedAt,
	}
}

func validUserID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func normalizeMessage(s string) (string, bool) {
	text, err := normalizeDetail(s, 1000)
	if err != nil || text == "" {
		return "", false
	}
	return text, true
}
