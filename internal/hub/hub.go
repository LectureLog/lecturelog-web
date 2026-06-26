// Package hub — доменный слой витрины публичных лекций (§8).
package hub

import "time"

// PublicLecture — публичная лекция витрины с автором.
type PublicLecture struct {
	ID              string
	OwnerID         string
	Title           string
	SourceKind      string
	PublishedAt     time.Time
	AuthorName      string
	AuthorAvatarURL string
}
