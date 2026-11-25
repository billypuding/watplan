package wat

import (
	"testing"

	"github.com/billypuding/watplan/internal/model"
)

func TestDirectoryResolvesOnlyUniquePeople(t *testing.T) {
	d := NewDirectory([]string{
		"dr Ważniewski Włodzimierz", "dr inż. Ważny Mariusz", "mgr Szemraj Tomasz", "ppłk mgr Jurzysta Paweł",
		"dr Stroińska Ewa", "Mielniczuk Łukasz", "mgr Buczyński Artur", "dr inż. Dziendzikowski Michał",
		"mgr inż. Dziewit Piotr", "dr hab. inż. Grzesik Norbert", "dr Jasiński Grzegorz",
	})
	for abbr, want := range map[string]string{
		"W.Waż":              "dr Ważniewski Włodzimierz",
		"T.Szem":             "mgr Szemraj Tomasz",
		"P.Jurz":             "ppłk mgr Jurzysta Paweł",
		"E.Stroi":            "dr Stroińska Ewa",
		"Bucz":               "mgr Buczyński Artur",
		"M.Miel":             "",
		"Dzie":               "",
		"Gr.":                "",
		"STANAG":             "",
		"M.KOCZ":             "",
		"mgr Szemraj Tomasz": "",
	} {
		if got, _ := d.Resolve(abbr); got != want {
			t.Errorf("%q → %q, chcę %q", abbr, got, want)
		}
	}
}

func TestResolveTeachersCopiesOnlyWhenChanged(t *testing.T) {
	d := NewDirectory([]string{"mgr Szemraj Tomasz"})
	events := []model.Event{{Teacher: "T.Szem"}, {Teacher: "dr Nowak Jan"}, {Teacher: "T.Szem, XyZ"}}
	out, n := ResolveTeachers(events, d)
	if n != 2 || out[0].Teacher != "mgr Szemraj Tomasz" || out[2].Teacher != "mgr Szemraj Tomasz, XyZ" {
		t.Fatalf("%d %+v", n, out)
	}
	if events[0].Teacher != "T.Szem" {
		t.Fatal("zmieniono wejściowe zdarzenia")
	}
	same, n := ResolveTeachers([]model.Event{{Teacher: "dr Nowak Jan"}}, d)
	if n != 0 || same[0].Teacher != "dr Nowak Jan" {
		t.Fatal("zmiana bez skrótu")
	}
}
