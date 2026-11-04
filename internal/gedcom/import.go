package gedcom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	gedlib "github.com/elliotchance/gedcom"

	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// Report summarizes one import run (shown to admins after import).
type Report struct {
	People   int64    `json:"people"`
	Families int64    `json:"families"`
	Sources  int64    `json:"sources"`
	Events   int64    `json:"events"`
	Media    int64    `json:"media"`
	Warnings []string `json:"warnings"`
}

const maxImportBytes int64 = 100 * 1024 * 1024 // 100 MB cap

// Import parses a GEDCOM stream and, in ONE SQLite transaction, replaces the
// current tree with it. The database either ends up fully replaced or
// untouched. Settings, users and import history survive the replacement.
func Import(ctx context.Context, st *store.Store, r io.Reader, filename string) (Report, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxImportBytes))
	if err != nil {
		return Report{}, fmt.Errorf("read input: %w", err)
	}
	if int64(len(raw)) >= maxImportBytes {
		return Report{}, fmt.Errorf("file exceeds %d MB limit", maxImportBytes/1024/1024)
	}

	utf8Data, warnings := normalizeCharset(raw)

	doc, err := gedlib.NewDecoder(bytes.NewReader(utf8Data)).Decode()
	if err != nil {
		recordFailedImport(ctx, st, filename, fmt.Errorf("parse GEDCOM: %w", err))
		return Report{}, fmt.Errorf("parse GEDCOM: %w", err)
	}
	if doc == nil || len(doc.Individuals()) == 0 {
		cause := fmt.Errorf("no individuals found — is this a GEDCOM file?")
		recordFailedImport(ctx, st, filename, cause)
		return Report{}, cause
	}

	rep := Report{Warnings: warnings}
	if err := writeAll(ctx, st, doc, filename, &rep); err != nil {
		recordFailedImport(ctx, st, filename, err)
		return rep, err
	}
	return rep, nil
}

// writeAll performs the replace-tree transaction and bumps the imports table.
func writeAll(ctx context.Context, st *store.Store, doc *gedlib.Document, filename string, rep *Report) error {
	tx, err := st.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()
	q := store.NewQueries(tx)

	for _, stmt := range []string{
		`DELETE FROM citations`, `DELETE FROM events`, `DELETE FROM child_links`,
		`DELETE FROM media`, `DELETE FROM families`, `DELETE FROM people`,
		`DELETE FROM sources`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("clear tree (%s): %w", stmt, err)
		}
	}

	// 1. Sources first (citations reference them by DB id).
	sourceIDs := map[string]int64{} // GEDCOM xref ("@S2@") -> db id
	for _, src := range doc.Sources() {
		if gedlib.IsNil(src) {
			continue
		}
		s := models.Source{
			GedcomXref:  normXref(src.Pointer()),
			Title:       src.Title(),
			Author:      childValue(src, "AUTH"),
			Publication: childValue(src, "PUBL"),
			Text:        childValue(src, "TEXT"),
		}
		id, err := q.UpsertSource(ctx, &s)
		if err != nil {
			return fmt.Errorf("source %s: %w", src.Pointer(), err)
		}
		sourceIDs[normXref(src.Pointer())] = id
	}
	rep.Sources = int64(len(sourceIDs))

	// 2. Individuals.
	personIDs := map[string]int64{}
	for _, ind := range doc.Individuals() {
		if ind == nil {
			continue
		}
		person, events, media, citations, warns := mapPerson(ind, sourceIDs)
		rep.Warnings = append(rep.Warnings, warns...)

		id, err := q.InsertPerson(ctx, &person)
		if err != nil {
			return fmt.Errorf("person %s: %w", ind.Pointer(), err)
		}
		personIDs[normXref(ind.Pointer())] = id
		rep.People++
		rep.Media += int64(len(media))

		for i := range events {
			events[i].PersonID = &id
			if _, err := q.InsertEvent(ctx, &events[i]); err != nil {
				return fmt.Errorf("event for person %s: %w", ind.Pointer(), err)
			}
			rep.Events++
		}
		for i := range media {
			media[i].PersonID = &id
			if _, err := q.InsertMedia(ctx, &media[i], nil, nil); err != nil {
				return fmt.Errorf("media for person %s: %w", ind.Pointer(), err)
			}
		}
		for i := range citations {
			citations[i].PersonID = &id
			if _, err := q.InsertCitation(ctx, &citations[i]); err != nil {
				return fmt.Errorf("citation for person %s: %w", ind.Pointer(), err)
			}
		}
	}

	// 3. Families (+ child links in GEDCOM order).
	for _, fam := range doc.Families() {
		if fam == nil {
			continue
		}
		family, events, media, citations, warns := mapFamily(fam, personIDs, sourceIDs)
		rep.Warnings = append(rep.Warnings, warns...)

		id, err := q.InsertFamily(ctx, &family)
		if err != nil {
			return fmt.Errorf("family %s: %w", fam.Pointer(), err)
		}
		rep.Families++
		rep.Media += int64(len(media))

		for i := range events {
			events[i].FamilyID = &id
			if _, err := q.InsertEvent(ctx, &events[i]); err != nil {
				return fmt.Errorf("event for family %s: %w", fam.Pointer(), err)
			}
			rep.Events++
		}
		for i := range media {
			media[i].FamilyID = &id
			if _, err := q.InsertMedia(ctx, &media[i], nil, nil); err != nil {
				return fmt.Errorf("media for family %s: %w", fam.Pointer(), err)
			}
		}
		for i := range citations {
			citations[i].FamilyID = &id
			if _, err := q.InsertCitation(ctx, &citations[i]); err != nil {
				return fmt.Errorf("citation for family %s: %w", fam.Pointer(), err)
			}
		}

		for _, child := range fam.Children().Individuals() {
			childID, ok := personIDs[normXref(child.Pointer())]
			if !ok {
				rep.Warnings = append(rep.Warnings,
					fmt.Sprintf("family %s: child %s not found", fam.Pointer(), child.Pointer()))
				continue
			}
			if _, err := q.AddChildLink(ctx, id, childID, models.ChildBirth); err != nil {
				return fmt.Errorf("child link %s->%s: %w", fam.Pointer(), child.Pointer(), err)
			}
		}
	}

	// Record the completed run (admin page reads this table).
	if _, err := tx.ExecContext(ctx, `INSERT INTO imports
(filename, status, people_count, families_count, sources_count, events_count, media_count, warnings, finished_at)
VALUES (?,?,?,?,?,?,?,?, datetime('now'))`,
		filename, "done", rep.People, rep.Families, rep.Sources, rep.Events,
		rep.Media, toJSON(rep.Warnings)); err != nil {
		return fmt.Errorf("record import run: %w", err)
	}

	return tx.Commit()
}

// recordFailedImport notes a failed run after the transaction rolled back.
func recordFailedImport(ctx context.Context, st *store.Store, filename string, cause error) {
	warnings := toJSON([]string{cause.Error()})
	_, _ = st.DB.ExecContext(ctx, `INSERT INTO imports
(filename, status, warnings, finished_at)
VALUES (?, 'failed', ?, datetime('now'))`, filename, warnings)
}

// --- node mapping ---------------------------------------------------------

// personCoreTags are mapped structurally; underscore-prefixed (vendor) tags
// and everything else event-like is stored generically.
var personSkipTags = map[string]bool{
	"NAME": true, "SEX": true, "BIRT": true, "DEAT": true, "BURI": true,
	"FAMS": true, "FAMC": true, "NOTE": true, "SOUR": true, "OBJE": true,
	"CHAN": true, "RFN": true, "RIN": true,
}

var familySkipTags = map[string]bool{
	"HUSB": true, "WIFE": true, "CHIL": true, "MARR": true, "DIV": true,
	"NOTE": true, "SOUR": true, "OBJE": true, "CHAN": true,
}

// eventTags is our own selector for life facts (the library's IsEvent is
// narrower than wanted: OCCU/RESI/EDUC are not in it).
var eventTags = map[string]bool{
	// person events (BIRT/DEAT/BURI are columns, handled before this)
	"ADOP": true, "BAPM": true, "BAPL": true, "BASM": true, "BLES": true,
	"CENS": true, "CHR": true, "CHRA": true, "CONF": true, "CREM": true,
	"EDUC": true, "EMIG": true, "EVEN": true, "FCOM": true, "GRAD": true,
	"IMMI": true, "NATU": true, "OCCU": true, "ORDN": true, "PROB": true,
	"RESI": true, "RETI": true, "WILL": true,
	// family events (MARR/DIV are fields, handled before this)
	"ANUL": true, "DIVF": true, "ENGA": true, "MARB": true, "MARC": true,
	"MARL": true, "MARS": true,
}

// isEventNode recognizes facts worth storing as events: known event tags,
// or unknown tags that carry a DATE/PLAC child (vendor extensions).
func isEventNode(n gedlib.Node) bool {
	t := n.Tag().Tag()
	if eventTags[t] {
		return true
	}
	for _, child := range n.Nodes() {
		switch child.Tag().Tag() {
		case "DATE", "PLAC":
			return true
		}
	}
	return false
}

// mapPerson flattens one IndividualNode into a person + tables of dependents.
func mapPerson(ind *gedlib.IndividualNode, sourceIDs map[string]int64) (
	models.Person, []models.Event, []models.Media, []models.Citation, []string) {
	var p models.Person
	var events []models.Event
	var media []models.Media
	var citations []models.Citation
	var warns []string

	p.GedcomXref = normXref(ind.Pointer())

	// Names.
	names := ind.Names()
	if len(names) > 0 {
		p.GivenNames = names[0].GivenName()
		p.Surname = names[0].Surname()
		if prefix := names[0].SurnamePrefix(); prefix != "" {
			p.Surname = strings.TrimSpace(prefix + " " + p.Surname)
		}
		if suffix := names[0].Suffix(); suffix != "" {
			p.GivenNames = strings.TrimSpace(p.GivenNames + " " + suffix)
		}
	}
	if len(names) > 1 {
		warns = append(warns,
			fmt.Sprintf("person %s has %d names; using the first", ind.Pointer(), len(names)))
	}

	// Married name (MyHeritage "_MARNM" on INDI or under NAME).
	p.MarriedName = customValue(ind, "_MARNM")
	if p.MarriedName == "" && len(names) > 0 {
		p.MarriedName = customValue(names[0], "_MARNM")
	}
	raw := map[string]string{}
	if len(names) > 0 {
		// Vendor tags under NAME (e.g. MyHeritage "_HEB" Hebrew name).
		for _, n := range names[0].Nodes() {
			if t := n.Tag().Tag(); strings.HasPrefix(t, "_") {
				raw[t] = n.Value()
			}
		}
	}

	// Sex.
	switch {
	case ind.Sex() != nil && ind.Sex().IsMale():
		p.Sex = models.SexMale
	case ind.Sex() != nil && ind.Sex().IsFemale():
		p.Sex = models.SexFemale
	default:
		p.Sex = models.SexUnknown
	}

	// Birth / death (denormalized onto people).
	if dateNode, placeNode := ind.Birth(); dateNode != nil || placeNode != nil {
		p.BirthDate = nodeValue(dateNode)
		p.BirthPlace = nodeValue(placeNode)
		p.BirthYear = yearOrNull(p.BirthDate)
	}
	if dateNode, placeNode := ind.Death(); dateNode != nil || placeNode != nil {
		p.DeathDate = nodeValue(dateNode)
		p.DeathPlace = nodeValue(placeNode)
		p.DeathYear = yearOrNull(p.DeathDate)
	}
	p.Living = models.IsLiving(p.DeathYear, p.BirthYear)

	// Remaining nodes (raw was seeded with NAME-level vendor tags).
	for _, n := range ind.Nodes() {
		tag := n.Tag().Tag()
		underscore := strings.HasPrefix(tag, "_")
		switch {
		case tag == "NOTE":
			p.Notes = appendNoteText(p.Notes, n)
		case tag == "OBJE":
			media = append(media, mediaFromNode(n)...)
		case tag == "SOUR":
			if c, warn := citationFromNode(n, sourceIDs, p.GedcomXref); warn != "" {
				warns = append(warns, warn)
			} else {
				citations = append(citations, c)
			}
		case personSkipTags[tag]:
			// handled above
		case underscore:
			raw[tag] = n.Value()
		case isEventNode(n):
			events = append(events, eventFromNode(n))
		default:
			raw[tag] = n.Value()
		}
	}
	if len(raw) > 0 {
		p.RawFacts = toJSON(raw)
	}
	return p, events, media, citations, warns
}

// mapFamily flattens one FamilyNode.
func mapFamily(fam *gedlib.FamilyNode, personIDs, sourceIDs map[string]int64) (
	models.Family, []models.Event, []models.Media, []models.Citation, []string) {
	var f models.Family
	var events []models.Event
	var media []models.Media
	var citations []models.Citation
	var warns []string

	f.GedcomXref = normXref(fam.Pointer())

	if h := fam.Husband(); h != nil {
		if id, ok := personIDs[normXref(h.Value())]; ok {
			f.Partner1ID = &id
		} else {
			warns = append(warns, fmt.Sprintf("family %s: husband %q not found", fam.Pointer(), h.Value()))
		}
	}
	if w := fam.Wife(); w != nil {
		if id, ok := personIDs[normXref(w.Value())]; ok {
			f.Partner2ID = &id
		} else {
			warns = append(warns, fmt.Sprintf("family %s: wife %q not found", fam.Pointer(), w.Value()))
		}
	}

	hasMarriage, hasDivorce := false, false
	for _, n := range fam.Nodes() {
		tag := n.Tag().Tag()
		switch tag {
		case "MARR":
			hasMarriage = true
			f.MarriageDate = childValue(n, "DATE")
			f.MarriageYear = yearOrNull(f.MarriageDate)
			f.MarriagePlace = childValue(n, "PLAC")
		case "DIV":
			hasDivorce = true
		case "NOTE":
			f.Notes = appendNoteText(f.Notes, n)
		case "SOUR":
			if c, warn := citationFromNode(n, sourceIDs, fam.Pointer()); warn != "" {
				warns = append(warns, warn)
			} else {
				citations = append(citations, c)
			}
		case "OBJE":
			media = append(media, mediaFromNode(n)...)
		default:
			if strings.HasPrefix(tag, "_") {
				rawAppendFamily(&f, tag, n.Value())
			} else if isEventNode(n) {
				events = append(events, eventFromNode(n))
			}
		}
	}
	switch {
	case hasMarriage && hasDivorce:
		f.Status = models.FamilyDivorced
	case hasMarriage:
		f.Status = models.FamilyMarried
	case len(fam.Children()) > 0:
		f.Status = models.FamilyUnmarried
	default:
		f.Status = models.FamilyOther
	}
	return f, events, media, citations, warns
}

// rawAppendFamily collects "_TAG"s into the family's raw_facts JSON.
func rawAppendFamily(f *models.Family, tag, value string) {
	raw := map[string]string{}
	if f.RawFacts != "" {
		_ = json.Unmarshal([]byte(f.RawFacts), &raw)
	}
	raw[tag] = value
	f.RawFacts = toJSON(raw)
}

// eventFromNode converts an event node (OCCU, RESI, EDU, CENS…) into a row.
func eventFromNode(n gedlib.Node) models.Event {
	var e models.Event
	e.Tag = n.Tag().Tag()
	e.Date = childValue(n, "DATE")
	e.Year = yearOrNull(e.Date)
	e.Place = childValue(n, "PLAC")

	var detail strings.Builder
	if v := childValue(n, "TYPE"); v != "" {
		detail.WriteString(v)
	}
	for _, child := range n.Nodes() {
		t := child.Tag().Tag()
		if t == "DATE" || t == "PLAC" || t == "TYPE" || t == "NOTE" || t == "SOUR" {
			continue
		}
		if child.Value() != "" {
			detail.WriteString(" " + child.Value())
		}
	}
	e.Detail = strings.TrimSpace(detail.String())
	return e
}

// mediaFromNode converts an OBJE node to filename reference rows (the media
// files are shipped separately by MyHeritage, so blobs are attached later).
func mediaFromNode(n gedlib.Node) []models.Media {
	var out []models.Media
	for _, file := range gedlib.NodesWithTag(n, gedlib.TagFile) {
		filename := file.Value()
		if filename == "" {
			continue
		}
		m := models.Media{Filename: filename, ImportNote: true}
		if titl := gedlib.NodesWithTag(n, gedlib.TagTitle); len(titl) > 0 {
			m.Caption = titl[0].Value()
		}
		out = append(out, m)
	}
	return out
}

// citationFromNode resolves a SOUR node: either a pointer ("@S2@") or an
// inline value.
func citationFromNode(n gedlib.Node, sourceIDs map[string]int64, ownerPtr string) (models.Citation, string) {
	if v := n.Value(); v != "" {
		if id, ok := sourceIDs[normXref(v)]; ok {
			return models.Citation{SourceID: id, Detail: childValue(n, "PAGE")}, ""
		}
		return models.Citation{}, fmt.Sprintf(
			"%s: source reference %q not found", ownerPtr, v)
	}
	return models.Citation{Detail: childValue(n, "PAGE")}, ""
}

// appendNoteText appends NOTE node text, joining CONT/CONC continuations.
func appendNoteText(current string, n gedlib.Node) string {
	var sb strings.Builder
	sb.WriteString(current)
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(n.Value())
	for _, child := range n.Nodes() {
		switch child.Tag().Tag() {
		case "CONT":
			sb.WriteString("\n" + child.Value())
		case "CONC":
			sb.WriteString(child.Value())
		}
	}
	return sb.String()
}

// --- tiny node helpers ----------------------------------------------------

func childValue(n gedlib.Node, tag string) string {
	nodes := gedlib.NodesWithTag(n, gedlib.TagFromString(tag))
	if len(nodes) == 0 {
		return ""
	}
	return nodes[0].Value()
}

// customValue returns the value of a "_TAG" child node, if present.
func customValue(n gedlib.Node, tag string) string {
	return childValue(n, tag)
}

// normXref normalizes GEDCOM references to canonical "@I1@" form: the
// library strips the @-signs from INDI/FAM pointers while reference values
// (HUSB @I1@) keep them.
func normXref(x string) string {
	x = strings.TrimSpace(x)
	if x == "" {
		return ""
	}
	if !strings.HasPrefix(x, "@") {
		return "@" + x + "@"
	}
	return x
}

func nodeValue(n gedlib.Node) string {
	// gedlib functions return typed-nil pointers held in non-nil interfaces,
	// so a plain == nil check is not enough.
	if gedlib.IsNil(n) {
		return ""
	}
	return n.Value()
}

func yearOrNull(raw string) *int64 {
	y := yearFromString(raw)
	if y == 0 {
		return nil
	}
	return &y
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
