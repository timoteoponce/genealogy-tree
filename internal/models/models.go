// Package models defines the application's data structures, shared by the
// store (SQL), the GEDCOM importer and the web handlers.
package models

import (
	"strconv"
	"time"
)

// Sex values as stored in people.sex ('X' = other/undisclosed).
const (
	SexMale    = "M"
	SexFemale  = "F"
	SexUnknown = "U"
	SexOther   = "X"
)

// Family statuses (families.status).
const (
	FamilyMarried   = "married"
	FamilyUnmarried = "unmarried"
	FamilyDivorced  = "divorced"
	FamilySeparated = "separated"
	FamilyOther     = "other"
)

// Child link relationships (child_links.relationship).
const (
	ChildBirth   = "birth"
	ChildAdopted = "adopted"
	ChildFoster  = "foster"
)

// Person is one individual. GedcomXref preserves the original "@I1@" tag so
// that re-imports can match people later. RawFacts is a JSON object holding
// GEDCOM tags the app does not model natively (nothing is discarded).
type Person struct {
	ID          int64
	GedcomXref  string
	GivenNames  string
	Surname     string
	MarriedName string
	Nickname    string
	Sex         string // M, F, U or X
	BirthDate   string
	BirthYear   *int64
	BirthPlace  string
	DeathDate   string
	DeathYear   *int64
	DeathPlace  string
	Living      bool
	Notes       string
	RawFacts    string
	CreatedAt   string
	UpdatedAt   string
}

// DisplayName is the friendly, sortable name ("John Smith", or "(unknown)").
func (p Person) DisplayName() string {
	n := (p.GivenNames + " " + p.Surname)
	if p.MarriedName != "" && p.MarriedName != p.Surname {
		n += " (" + p.MarriedName + ")"
	}
	if len(p.GivenNames) == 0 && len(p.Surname) == 0 {
		return "(unknown)"
	}
	return n
}

// FullNameFor list pages: name plus life-spans, e.g. "John Smith (1847–1912)".
func (p Person) Years() string {
	b, d := "", ""
	if p.BirthYear != nil {
		b = trimYear(*p.BirthYear)
	}
	if p.DeathYear != nil {
		d = trimYear(*p.DeathYear)
	}
	switch {
	case b == "" && d == "":
		return ""
	case d == "":
		return b + "–" // born, death year not recorded
	default:
		return b + "–" + d
	}
}

func trimYear(y int64) string {
	return strconv.Itoa(int(y))
}

// IsLiving recomputes the "living" heuristic: no death recorded and either
// no birth year or born within the last ~110 years.
func IsLiving(deathYear *int64, birthYear *int64) bool {
	if deathYear != nil {
		return false
	}
	if birthYear == nil {
		return true // no dates at all: assume living (safe default)
	}
	cutoff := int64(time.Now().Year() - 110)
	return *birthYear > cutoff
}

// Family is a couple that can share children via child_links.
type Family struct {
	ID            int64
	GedcomXref    string
	Partner1ID    *int64
	Partner2ID    *int64
	Status        string
	MarriageDate  string
	MarriageYear  *int64
	MarriagePlace string
	Notes         string
	RawFacts      string
	CreatedAt     string
	UpdatedAt     string
}

// Event is one generic life event / fact of a person or family
// (tag: OCCU, RESI, EDU, BAPM, DIV…). BIRT/DEAT/MARR are denormalized
// onto people/families, so they do not usually appear here.
type Event struct {
	ID       int64
	PersonID *int64
	FamilyID *int64
	Tag      string
	Date     string
	Year     *int64
	Place    string
	Detail   string
	Sort     int64
}

// ChildLink connects a person to their parents' family.
type ChildLink struct {
	ID           int64
	FamilyID     int64
	PersonID     int64
	Ord          int64
	Relationship string
}

// Media is one photo/media object. BLOBs (Data, Thumb) are loaded on demand
// — never select them in list queries.
type Media struct {
	ID         int64
	PersonID   *int64
	FamilyID   *int64
	Filename   string
	Mime       string
	Size       int64
	Caption    string
	IsProfile  bool
	ImportNote bool
	CreatedAt  string
	UpdatedAt  string
}

// MediaData is the selectable binary payload of a Media row.
type MediaData struct {
	Data  []byte // original upload (nil for import_note rows)
	Thumb []byte // generated 256px JPEG (nil for import_note rows)
	Mime  string
}

// Source and Citation are import-only objects (GEDCOM SOUR / SOURce refs).
type Source struct {
	ID          int64
	GedcomXref  string
	Title       string
	Author      string
	Publication string
	Text        string
}

type Citation struct {
	ID       int64
	SourceID int64
	PersonID *int64
	FamilyID *int64
	Detail   string
}

// User is a login account. Role is "admin", "editor" or "viewer".
type User struct {
	ID           int64
	Username     string
	PasswordHash string // bcrypt
	Role         string
	CreatedAt    string
	UpdatedAt    string
}

// Role weights for require-role comparisons.
var RoleWeights = map[string]int{
	"viewer": 1,
	"editor": 2,
	"admin":  3,
}

// ImportRun records one GEDCOM import (status: running/done/failed/rolled_back).
type ImportRun struct {
	ID            int64
	Filename      string
	Status        string
	PeopleCount   int64
	FamiliesCount int64
	SourcesCount  int64
	EventsCount   int64
	MediaCount    int64
	Warnings      string // JSON array
	StartedAt     string
	FinishedAt    *string
}

// AuditEntry is one row of the who-did-what log (admin-visible).
type AuditEntry struct {
	ID       int64
	UserID   *int64
	Action   string
	Entity   string
	EntityID *int64
	Detail   string
	At       string
}
