// Package store owns every SQL statement in Filiation.
//
// See embed.go for the package contract and the single-writer rules. Files here
// are organised by table group (works, edges, chunks, jobs) rather than by
// operation, so the SQL touching one part of the schema stays in one place.
package store
