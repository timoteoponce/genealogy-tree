package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"genealogy-tree/internal/db"
	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// openTestStore opens a fresh database in a temp dir (real file, so WAL
// and FK pragmas behave exactly like production).
func openTestStore(t *testing.T) (*store.Store, *store.Queries) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	s := store.New(d)
	return s, s.Queries()
}

func intPtr(v int64) *int64 { return &v }

func TestMigrationRunsOnce(t *testing.T) {
	s, _ := openTestStore(t)
	// Re-running Migrate on the same DB must be a no-op.
	if err := db.Migrate(s.DB); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM schema_version`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("want exactly 1 applied migration, got %d (err %v)", n, err)
	}
}

func TestPersonCRUD(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()

	person := models.Person{
		GivenNames: "John", Surname: "Smith", Sex: models.SexMale,
		BirthDate: "ABT 1847", BirthYear: intPtr(1847), BirthPlace: "Boston",
		Notes: "blacksmith",
	}
	id, err := q.InsertPerson(ctx, &person)
	if err != nil {
		t.Fatal(err)
	}
	if person.Living {
		t.Error("person with birth 1847 and no death should NOT be living (older than 110)")
	}
	person.ID = id

	got, err := q.PersonByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName() != "John Smith" {
		t.Errorf("DisplayName = %q", got.DisplayName())
	}
	if got.BirthYear == nil || *got.BirthYear != 1847 {
		t.Errorf("birth year not persisted")
	}
	if got.CreatedAt == "" {
		t.Errorf("created_at not set by default")
	}

	got.GivenNames = "Johnny"
	if err := q.UpdatePerson(ctx, &got); err != nil {
		t.Fatal(err)
	}
	got2, _ := q.PersonByID(ctx, id)
	if got2.GivenNames != "Johnny" {
		t.Errorf("update not applied")
	}

	if err := q.DeletePerson(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := q.PersonByID(ctx, id); err == nil {
		t.Error("person should be gone")
	}
}

func TestSearchPeople(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()
	for _, p := range []models.Person{
		{GivenNames: "Anna", Surname: "Kovács", Sex: models.SexFemale},
		{GivenNames: "Bob", MarriedName: "Kovács", Surname: "Dole", Sex: models.SexMale},
		{GivenNames: "Cid", Surname: "Alvarez", Sex: models.SexMale},
	} {
		if _, err := q.InsertPerson(ctx, &p); err != nil {
			t.Fatal(err)
		}
	}

	people, total, err := q.SearchPeople(ctx, "kovács", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 || total != 2 {
		t.Fatalf("want 2 matches, got %d (total %d)", len(people), total)
	}
	if people[0].Surname != "Dole" {
		t.Errorf("order should be by own surname, got %s", people[0].Surname)
	}

	// Pagination
	page, _, err := q.SearchPeople(ctx, "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("want page of 1 person, got %d", len(page))
	}
}

func TestFamilyAndChildren(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()

	mkPerson := func(name string) models.Person {
		p := models.Person{GivenNames: name, Sex: models.SexUnknown}
		id, _ := q.InsertPerson(ctx, &p)
		p.ID = id
		return p
	}
	father := mkPerson("Frank")
	mother := mkPerson("May")
	child1 := mkPerson("Cody")
	child2 := mkPerson("Cindy")

	fam := models.Family{Partner1ID: &father.ID, Partner2ID: &mother.ID, Status: models.FamilyMarried}
	famID, err := q.InsertFamily(ctx, &fam)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.AddChildLink(ctx, famID, child1.ID, models.ChildBirth); err != nil {
		t.Fatal(err)
	}
	if _, err := q.AddChildLink(ctx, famID, child2.ID, models.ChildAdopted); err != nil {
		t.Fatal(err)
	}

	kids, err := q.ChildrenOfFamily(ctx, famID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 || kids[0].GivenNames != "Cody" || kids[1].GivenNames != "Cindy" {
		t.Fatalf("children order: %+v", kids)
	}

	parents, err := q.FamiliesOfChild(ctx, child1.ID)
	if err != nil || len(parents) != 1 || parents[0].ID != famID {
		t.Fatalf("FamiliesOfChild: %v %+v", err, parents)
	}
	asPartner, err := q.FamiliesOfPerson(ctx, father.ID)
	if err != nil || len(asPartner) != 1 {
		t.Fatalf("FamiliesOfPerson: %v %+v", err, asPartner)
	}

	// Delete family removes child links (cascade).
	if err := q.DeleteFamily(ctx, famID); err != nil {
		t.Fatal(err)
	}
	if kids, _ := q.ChildrenOfFamily(ctx, famID); len(kids) != 0 {
		t.Error("children not cascaded")
	}
}

func TestPersonDeletePrunesFamilies(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()

	only := models.Person{GivenNames: "Lonely", Sex: models.SexUnknown}
	id, _ := q.InsertPerson(ctx, &only)

	fam := models.Family{Partner1ID: &id}
	famID, _ := q.InsertFamily(ctx, &fam)
	_ = famID

	if err := q.DeletePerson(ctx, id); err != nil {
		t.Fatal(err)
	}
	n, err := q.PruneEmptyFamilies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected to prune exactly 1 empty family, pruned %d", n)
	}
}

func TestEvents(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()

	p := models.Person{GivenNames: "Eve", Sex: models.SexFemale}
	id, _ := q.InsertPerson(ctx, &p)

	e := models.Event{PersonID: &id, Tag: "OCCU", Date: "BET 1850 AND 1860", Detail: "carpenter", Sort: 1}
	if _, err := q.InsertEvent(ctx, &e); err != nil {
		t.Fatal(err)
	}
	events, err := q.ListEventsForPerson(ctx, id)
	if err != nil || len(events) != 1 || events[0].Detail != "carpenter" {
		t.Fatalf("events: %v %+v", err, events)
	}
	if err := q.DeleteEventsForPerson(ctx, id); err != nil {
		t.Fatal(err)
	}
	if events, _ := q.ListEventsForPerson(ctx, id); len(events) != 0 {
		t.Error("events not deleted")
	}
}

func TestMediaAndProfile(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()

	p := models.Person{GivenNames: "Picasso", Sex: models.SexMale}
	id, _ := q.InsertPerson(ctx, &p)

	m1 := models.Media{PersonID: &id, Filename: "a.jpg", Mime: "image/jpeg", Size: 10}
	m1ID, err := q.InsertMedia(ctx, &m1, []byte("data1"), []byte("thumb1"))
	if err != nil {
		t.Fatal(err)
	}
	m2 := models.Media{PersonID: &id, Filename: "b.jpg", Mime: "image/jpeg"}
	m2ID, err := q.InsertMedia(ctx, &m2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m2.ImportNote {
		t.Error("placeholder without blob must be flagged ImportNote")
	}
	m2.ID = m2ID

	if err := q.SetProfilePhoto(ctx, id, m1ID); err != nil {
		t.Fatal(err)
	}
	ph, err := q.ProfilePhoto(ctx, id)
	if err != nil || ph.ID != m1ID {
		t.Fatalf("profile = %+v %v", ph, err)
	}

	data, err := q.MediaData(ctx, m1ID)
	if err != nil || string(data.Data) != "data1" || string(data.Thumb) != "thumb1" {
		t.Fatalf("media data = %+v %v", data, err)
	}

	listed, err := q.ListMediaForPerson(ctx, id)
	if err != nil || len(listed) != 2 {
		t.Fatalf("media list = %+v %v", listed, err)
	}
	if !listed[0].IsProfile {
		t.Error("profile photo must list first")
	}

	if err := q.DeleteMedia(ctx, m2.ID); err != nil {
		t.Fatal(err)
	}
	if cnt, _ := q.CountMedia(ctx); cnt != 1 {
		t.Errorf("count after delete = %d", cnt)
	}
}

func TestSourcesCitationsSettingsAudit(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()

	src := models.Source{GedcomXref: "@S1@", Title: "Census 1900"}
	id1, err := q.UpsertSource(ctx, &src)
	if err != nil {
		t.Fatal(err)
	}
	// Upsert again with same xref must not create a second row.
	src2 := models.Source{GedcomXref: "@S1@", Title: "Census 1900 (fixed)"}
	id2, err := q.UpsertSource(ctx, &src2)
	if err != nil || id2 != id1 {
		t.Fatalf("upsert should reuse row: %d vs %d (%v)", id2, id1, err)
	}

	p := models.Person{GivenNames: "Ann", Sex: models.SexFemale}
	annID, _ := q.InsertPerson(ctx, &p)
	cit := models.Citation{SourceID: id1, PersonID: &annID, Detail: "line 3"}
	if _, err := q.InsertCitation(ctx, &cit); err != nil {
		t.Fatal(err)
	}
	cits, err := q.ListCitationsForPerson(ctx, annID)
	if err != nil || len(cits) != 1 || cits[0].SourceTitle != "Census 1900 (fixed)" {
		t.Fatalf("citations: %+v %v", cits, err)
	}

	if err := q.SetSetting(ctx, "site_name", "Worley Family"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := q.GetSetting(ctx, "site_name"); !ok || v != "Worley Family" {
		t.Fatalf("setting roundtrip failed")
	}
	if _, ok, _ := q.GetSetting(ctx, "missing"); ok {
		t.Error("missing setting should not report ok")
	}

	if err := q.InsertAudit(ctx, q.DB, nil, "import", "gedcom", nil, "test"); err != nil {
		t.Fatal(err)
	}
	log, err := q.RecentAudit(ctx, 10)
	if err != nil || len(log) != 1 {
		t.Fatalf("audit entries: %+v %v", log, err)
	}
}

func TestLivingCounter(t *testing.T) {
	_, q := openTestStore(t)
	ctx := context.Background()
	for _, p := range []models.Person{
		{GivenNames: "Old", BirthYear: intPtr(1800), Sex: models.SexUnknown}, // dead
		{GivenNames: "Young", BirthYear: intPtr(2000), Sex: models.SexUnknown},
	} {
		if _, err := q.InsertPerson(ctx, &p); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := q.LivingCount(ctx); n != 1 {
		t.Errorf("living count = %d, want 1", n)
	}
}
