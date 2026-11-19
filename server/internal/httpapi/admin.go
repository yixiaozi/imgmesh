package httpapi

import (
	"crypto/subtle"
	"net/http"
	"time"
)

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	if s.AdminToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, errBody{"admin_unconfigured"})
		return
	}
	got := r.Header.Get("X-Imgmesh-Admin")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.AdminToken)) != 1 {
		writeJSON(w, http.StatusUnauthorized, errBody{"unauthorized"})
		return
	}
	overview, err := s.Store.AdminOverview(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	people := make([]any, 0, len(overview.People))
	for _, person := range overview.People {
		people = append(people, map[string]any{
			"name": person.Name, "city": person.City, "platform": person.Platform,
			"deviceId": person.DeviceID, "createdAt": person.CreatedAt,
		})
	}
	shares := make([]any, 0, len(overview.ShareList))
	for _, share := range overview.ShareList {
		shares = append(shares, map[string]any{
			"id": share.ID, "owner": share.Owner, "photos": share.Photos,
			"bytes": share.Bytes, "stored": share.Stored, "revoked": share.Revoked, "createdAt": share.CreatedAt,
		})
	}
	photos := make([]any, 0, len(overview.Photos))
	for _, photo := range overview.Photos {
		photos = append(photos, map[string]any{
			"owner": photo.Owner, "bytes": photo.Bytes, "stored": photo.Stored, "status": photo.Status, "at": photo.At,
		})
	}
	events := make([]any, 0, len(overview.Events))
	for _, event := range overview.Events {
		events = append(events, map[string]any{
			"at": event.At, "kind": event.Kind, "client": event.Client, "name": event.Name,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users": overview.Users, "devices": overview.Devices, "shares": overview.Shares,
		"photosTransferred": overview.PhotosTransferred,
		"storageBytes":      overview.StorageBytes,
		"people":            people, "shareList": shares, "photos": photos, "events": events,
		"updatedAt": time.Now().UTC(),
	})
}
