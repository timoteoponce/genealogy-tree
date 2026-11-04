package gedcom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"genealogy-tree/internal/db"
	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "gedcom-test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return store.New(d)
}

func importFile(t *testing.T, st *store.Store, path string) Report {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rep, err := Import(context.Background(), st, f, filepath.Base(path))
	if err != nil {
		t.Fatalf("import %s: %v", path, err)
	}
	return rep
}

func TestImportMinimal(t *testing.T) {
	st := openStore(t)
	rep := importFile(t, st, "../../testdata/gedcom/minimal.ged")

	if rep.People != 3 || rep.Families != 1 || rep.Events != 1 {
		t.Fatalf("counts: %+v", rep)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", rep.Warnings)
	}

	ctx := context.Background()
	q := st.Queries()

	p, err := q.PersonByXref(ctx, "@I1@")
	if err != nil {
		t.Fatalf("person @I1@: %v", err)
	}
	if p.DisplayName() != "John Smith" || p.Sex != models.SexMale {
		t.Fatalf("person: %s sex=%s", p.DisplayName(), p.Sex)
	}
	if p.BirthYear == nil || *p.BirthYear != 1847 || p.BirthPlace != "Boston, Massachusetts" {
		t.Fatalf("birth: %q %+v", p.BirthDate, p.BirthYear)
	}
	if p.DeathYear == nil || *p.DeathYear != 1912 {
		t.Fatalf("death: %q %+v", p.DeathDate, p.DeathYear)
	}
	if !strings.Contains(p.Notes, "He loved the forge.") {
		t.Errorf("CONT note not merged: %q", p.Notes)
	}

	events, _ := q.ListEventsForPerson(ctx, p.ID)
	ok := false
	for _, e := range events {
		if e.Tag == "OCCU" && e.Place == "Boston, Massachusetts" && e.Detail == "Blacksmith" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("OCCU event missing: %+v", events)
	}

	fams, _ := q.FamiliesOfPerson(ctx, p.ID)
	if len(fams) != 1 || fams[0].MarriageYear == nil || *fams[0].MarriageYear != 1875 {
		t.Fatalf("family of @I1@: %+v", fams)
	}
	kids, _ := q.ChildrenOfFamily(ctx, fams[0].ID)
	if len(kids) != 1 || kids[0].GivenNames != "Samuel" {
		t.Fatalf("children: %+v", kids)
	}

	samuel, _ := q.PersonByXref(ctx, "@I3@")
	fs, _ := q.FamiliesOfChild(ctx, samuel.ID)
	if len(fs) != 1 {
		t.Fatalf("FamiliesOfChild: %+v", fs)
	}
}

func TestImportMyHeritageFlavor(t *testing.T) {
	st := openStore(t)
	rep := importFile(t, st, "../../testdata/gedcom/myheritage.ged")

	if rep.People != 3 || rep.Families != 1 || rep.Sources != 2 {
		t.Fatalf("counts: %+v", rep)
	}

	ctx := context.Background()
	q := st.Queries()

	p, _ := q.PersonByXref(ctx, "@I1@")
	if p.MarriedName != "Varga" {
		t.Errorf("_MARNM not imported: %q", p.MarriedName)
	}
	if p.DisplayName() != "Iszák Ádám Kovács (Varga)" {
		t.Errorf("name: %q", p.DisplayName())
	}

	var raw map[string]string
	if err := json.Unmarshal([]byte(p.RawFacts), &raw); err != nil || raw["_HEB"] == "" {
		t.Errorf("raw facts: %q err %v", p.RawFacts, err)
	}

	// OCCU, EDUC x2, RESI, CHR, BAPM = 6 events.
	events, _ := q.ListEventsForPerson(ctx, p.ID)
	if len(events) != 6 {
		t.Fatalf("events for @I1@: %d %+v", len(events), events)
	}
	// BET/CAL dates import with their first parsed year.
	var birth *int64
	born, _ := q.PersonByXref(ctx, "@I3@")
	birth = born.BirthYear
	if birth == nil || *birth != 1962 {
		t.Errorf("BET date year: %+v", birth)
	}

	media, _ := q.ListMediaForPerson(ctx, p.ID)
	if len(media) != 2 || !media[0].ImportNote {
		t.Fatalf("media: %+v", media)
	}
	if media[0].Caption != "Wedding photo" || media[0].Filename != "People/photo_12345.jpg" {
		t.Errorf("media fields: %+v", media[0])
	}

	cits, _ := q.ListCitationsForPerson(ctx, p.ID)
	if len(cits) != 1 || cits[0].SourceTitle != "Hungarian Civil Registration, 1895-1980" {
		t.Fatalf("citations: %+v", cits)
	}

	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "@S99@") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing warning for @S99@: %v", rep.Warnings)
	}
}

func TestImportEdgeCases(t *testing.T) {
	st := openStore(t)
	rep := importFile(t, st, "../../testdata/gedcom/edge.ged")

	if rep.People != 2 || rep.Families != 1 {
		t.Fatalf("counts: %+v", rep)
	}
	ctx := context.Background()
	q := st.Queries()

	p, err := q.PersonByXref(ctx, "@EDGE1@")
	if err != nil {
		t.Fatal(err)
	}
	if p.Sex != models.SexUnknown {
		t.Errorf("sex N should map to unknown: %q", p.Sex)
	}
	if p.BirthYear != nil {
		t.Errorf("DATE Y should not parse a year: %v", p.BirthYear)
	}

	// Family without MARR but with children →未婚 unmarried.
	edge2, _ := q.PersonByXref(ctx, "@EDGE2@")
	fams, _ := q.FamiliesOfChild(ctx, edge2.ID)
	if len(fams) != 1 || fams[0].Status != models.FamilyUnmarried {
		t.Fatalf("family status: %+v", fams)
	}
}

func TestImportReplacesTree(t *testing.T) {
	st := openStore(t)
	importFile(t, st, "../../testdata/gedcom/myheritage.ged")

	// A non-GEDCOM file must fail cleanly and record the failed run.
	bad := filepath.Join(t.TempDir(), "bad.ged")
	if err := os.WriteFile(bad, []byte("this is not a gedcom file"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(bad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Import(context.Background(), st, f, "bad.ged"); err == nil {
		t.Error("garbage import should fail")
	}
	f.Close()

	rep := importFile(t, st, "../../testdata/gedcom/minimal.ged")
	if rep.People != 3 {
		t.Fatalf("second import counts: %+v", rep)
	}
	ctx := context.Background()
	q := st.Queries()
	if n, _ := q.CountPeople(ctx); n != 3 {
		t.Errorf("people after replace: %d", n)
	}
	if n, _ := q.CountFamilies(ctx); n != 1 {
		t.Errorf("families after replace: %d", n)
	}

	var runs int64
	if err := st.DB.QueryRow(`SELECT count(*) FROM imports`).Scan(&runs); err != nil || runs < 2 {
		t.Fatalf("imports rows: %d %v", runs, err)
	}
	var failed int64
	if err := st.DB.QueryRow(`SELECT count(*) FROM imports WHERE status='failed'`).Scan(&failed); err != nil || failed != 1 {
		t.Fatalf("failed run not recorded: %d %v", failed, err)
	}
}

func TestCharsetDetection(t *testing.T) {
	// UTF-8 passthrough (declared).
	if out, _ := normalizeCharset([]byte("1 CHAR UTF-8\nnaïve")); !strings.Contains(string(out), "naïve") {
		t.Errorf("utf8: %q", out)
	}

	// Windows-1252 é (0xE9 single byte).
	out, _ := normalizeCharset([]byte("1 CHAR ANSI\ncaf\xE9"))
	if !strings.Contains(string(out), "café") {
		t.Errorf("ansi decode: %q", out)
	}

	// UTF-16LE with BOM and declared UNICODE.
	text := "0 HEAD\n1 CHAR UNICODE\n"
	u16 := []byte{0xFF, 0xFE}
	for _, r := range utf16.Encode([]rune(text)) {
		u16 = append(u16, byte(r), byte(r>>8))
	}
	outU, warnsU := normalizeCharset(u16)
	if string(outU) != text {
		t.Errorf("utf16: %q (warns %v)", outU, warnsU)
	}

	// ANSEL: combining grave (0xE1) precedes the base letter.
	outA, _ := normalizeCharset([]byte("1 CHAR ANSEL\nx\xE1abc"))
	if !strings.Contains(string(outA), "a\u0300bc") {
		t.Errorf("ansel combining: %q", outA)
	}

	// ANSEL base letter Ł (0xA1).
	gears := anselToUTF8([]byte{0xA1}, new([]string))
	if !strings.Contains(string(gears), "Ł") {
		t.Error("ansel Ł")
	}
}

func TestYearFromString(t *testing.T) {
	cases := map[string]int64{
		"ABT 1847":          1847,
		"BET 1900 AND 1905": 1900,
		"JUL 1876":          1876,
		"12 OCT 2023":       2023,
		"Y":                 0,
		"Bet 10 Mei 1712":   1712,
	}
	for raw, want := range cases {
		if got := yearFromString(raw); got != want {
			t.Errorf("yearFromString(%q) = %d, want %d", raw, got, want)
		}
	}
}
