package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/yixiaozi/imgmesh/server/internal/id"
	"github.com/yixiaozi/imgmesh/server/internal/store"
)

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	photoID, err := canonicalFingerprint(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	comments, err := s.Store.Comments(r.Context(), photoID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": commentJSON(comments, userID(r))})
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	photoID, err := canonicalFingerprint(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	text, err := normalizeDetail(body.Body, 200)
	if err != nil || text == "" {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	comment, err := s.Store.AddComment(r.Context(), photoID, userID(r), text)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, commentJSON([]store.PhotoComment{comment}, userID(r))[0])
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	photoID, err := canonicalFingerprint(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if err := s.Store.DeleteComment(r.Context(), photoID, chi.URLParam(r, "commentId"), userID(r)); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func commentJSON(comments []store.PhotoComment, viewer string) []any {
	out := make([]any, 0, len(comments))
	for _, comment := range comments {
		out = append(out, map[string]any{
			"id":          comment.ID,
			"userId":      comment.UserID,
			"displayName": comment.DisplayName,
			"body":        comment.Body,
			"createdAt":   comment.CreatedAt,
			"mine":        comment.UserID == viewer,
		})
	}
	return out
}

func (s *Server) activityFeed(w http.ResponseWriter, r *http.Request) {
	items, unread, err := s.Store.Feed(r.Context(), userID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		text := feedText(item.Kind)
		summary := ""
		if item.Kind == "message" {
			summary = clipRunes(item.Summary, 40)
		}
		out = append(out, map[string]any{
			"id":      item.ID,
			"name":    item.Name,
			"text":    text,
			"summary": summary,
			"at":      item.At,
			"read":    item.Read,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": unread, "items": out})
}

func feedText(kind string) string {
	switch kind {
	case "view", "open":
		return "打开了你的分享"
	case "download":
		return "下载了你的分享"
	case "save":
		return "保存了你的分享"
	case "append":
		return "往你的链接里加了照片"
	case "like":
		return "赞了你的照片"
	case "friend":
		return "想加你为好友"
	case "message":
		return "发来一条消息"
	default:
		return "有一件新动态"
	}
}

func clipRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func (s *Server) readActivity(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Store.MarkFeed(r.Context(), userID(r), body.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setRecovery(w http.ResponseWriter, r *http.Request) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
		return
	}
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	code := make([]byte, 16)
	for i := range code {
		code[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	sum := sha256.Sum256(code)
	if err := s.Store.SetRecovery(r.Context(), userID(r), sum[:]); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": string(code)})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.Store.Devices(r.Context(), userID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(devices))
	for _, device := range devices {
		out = append(out, map[string]any{
			"id": device.ID, "platform": device.Platform, "createdAt": device.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) recoverDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID string `json:"deviceId"`
		Code     string `json:"code"`
		Platform string `json:"platform"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !validDevice(body.DeviceID) || (body.Platform != "ios" && body.Platform != "web" && body.Platform != "desktop") {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	code := []byte(strings.ToUpper(strings.Join(strings.Fields(body.Code), "")))
	if len(code) < 8 || len(code) > 32 {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_code"})
		return
	}
	sum := sha256.Sum256(code)
	userID, err := s.Store.UserByRecovery(r.Context(), sum[:])
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.Store.AttachDevice(r.Context(), body.DeviceID, userID, body.Platform); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.writeSession(w, r, userID, true)
}

func (s *Server) appendShare(w http.ResponseWriter, r *http.Request) (store.Share, bool) {
	shareID, err := id.Decode16(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return store.Share{}, false
	}
	sh, err := s.Store.Share(r.Context(), shareID)
	if err != nil {
		writeStoreErr(w, err)
		return store.Share{}, false
	}
	if err := sh.Unavailable(); err != nil {
		writeStoreErr(w, err)
		return store.Share{}, false
	}
	if sh.OwnerID == userID(r) {
		return sh, true
	}
	tag, err := id.Decode32(r.Header.Get("X-Imgmesh-Access"))
	if err != nil || subtleCompare(tag, sh.AccessTag) != 1 {
		writeJSON(w, http.StatusUnauthorized, errBody{"access_denied"})
		return store.Share{}, false
	}
	return sh, true
}

func subtleCompare(a, b []byte) int {
	if len(a) != len(b) {
		return 0
	}
	diff := 0
	for i := range a {
		diff |= int(a[i] ^ b[i])
	}
	if diff == 0 {
		return 1
	}
	return 0
}
