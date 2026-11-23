package models

import (
	"strconv"
	"testing"
	"time"
)

func yearPtr(y int64) *int64 { return &y }

func TestPersonAge(t *testing.T) {
	thisYear := int64(time.Now().Year())
	bornThisYear := strconv.FormatInt(thisYear-40, 10)

	cases := []struct {
		name string
		p    Person
		want string
	}{
		{
			name: "age at death",
			p:    Person{BirthYear: yearPtr(1847), DeathYear: yearPtr(1912), BirthDate: "1847", DeathDate: "1912"},
			want: "65",
		},
		{
			name: "approximate death date",
			p:    Person{BirthYear: yearPtr(1847), DeathYear: yearPtr(1912), BirthDate: "1847", DeathDate: "ABT 1912"},
			want: "~65",
		},
		{
			name: "approximate birth range",
			p:    Person{BirthYear: yearPtr(1900), DeathYear: yearPtr(1960), BirthDate: "BET 1900 AND 1905", DeathDate: "1960"},
			want: "~60",
		},
		{
			name: "living person",
			p:    Person{BirthYear: yearPtr(thisYear - 40), Living: true, BirthDate: bornThisYear},
			want: "40",
		},
		{
			name: "no birth year",
			p:    Person{DeathYear: yearPtr(1912)},
			want: "",
		},
		{
			name: "deceased without death year",
			p:    Person{BirthYear: yearPtr(1855), BirthDate: "1855"},
			want: "",
		},
		{
			name: "death before birth",
			p:    Person{BirthYear: yearPtr(1900), DeathYear: yearPtr(1880)},
			want: "",
		},
		{
			name: "no dates at all",
			p:    Person{},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Age(); got != tc.want {
				t.Fatalf("Age() = %q, want %q", got, tc.want)
			}
		})
	}
}
