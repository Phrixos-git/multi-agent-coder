package intervals

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeContract(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	for _, tc := range []struct {
		name     string
		in, want []Interval
		invalid  bool
	}{
		{name: "nil"},
		{name: "empty", in: []Interval{}},
		{name: "singleton", in: []Interval{{4, 4}}, want: []Interval{{4, 4}}},
		{name: "sort and merge", in: []Interval{{8, 10}, {1, 3}, {3, 5}, {6, 6}}, want: []Interval{{1, 6}, {8, 10}}},
		{name: "adjacent", in: []Interval{{4, 6}, {1, 3}}, want: []Interval{{1, 6}}},
		{name: "nested duplicates", in: []Interval{{1, 10}, {2, 3}, {1, 10}}, want: []Interval{{1, 10}}},
		{name: "negative", in: []Interval{{0, 2}, {-4, -2}, {-1, -1}}, want: []Interval{{-4, 2}}},
		{name: "max overlap", in: []Interval{{maxInt, maxInt}, {maxInt - 2, maxInt}}, want: []Interval{{maxInt - 2, maxInt}}},
		{name: "max adjacent", in: []Interval{{maxInt, maxInt}, {maxInt - 1, maxInt - 1}}, want: []Interval{{maxInt - 1, maxInt}}},
		{name: "min adjacent", in: []Interval{{minInt + 1, minInt + 1}, {minInt, minInt}}, want: []Interval{{minInt, minInt + 1}}},
		{name: "full range", in: []Interval{{minInt, maxInt}, {0, 0}}, want: []Interval{{minInt, maxInt}}},
		{name: "far apart", in: []Interval{{maxInt, maxInt}, {minInt, minInt}}, want: []Interval{{minInt, minInt}, {maxInt, maxInt}}},
		{name: "invalid", in: []Interval{{3, 2}}, invalid: true},
		{name: "invalid after sortable prefix", in: []Interval{{8, 9}, {1, 2}, {5, 4}}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]Interval(nil), tc.in...)
			got, err := Normalize(tc.in)
			assertUnchanged(t, tc.in, before)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidInterval) || got != nil {
					t.Fatalf("got %v, %v; want nil, ErrInvalidInterval", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) || len(got) > 0 && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if len(got) > 0 {
				got[0].Start = 123
				assertUnchanged(t, tc.in, before)
			}
		})
	}
}

func assertUnchanged(t *testing.T, got, before []Interval) {
	t.Helper()
	if len(got) != len(before) {
		t.Fatal("input length changed")
	}
	for i := range before {
		if got[i] != before[i] {
			t.Fatalf("input mutated: %v, before %v", got, before)
		}
	}
}

func TestNormalizeExhaustive(t *testing.T) {
	var candidates []Interval
	for start := -3; start <= 3; start++ {
		for end := start; end <= 3; end++ {
			candidates = append(candidates, Interval{start, end})
		}
	}
	for _, a := range candidates {
		for _, b := range candidates {
			input := []Interval{a, b}
			got, err := Normalize(input)
			if err != nil {
				t.Fatal(err)
			}
			assertUnchanged(t, input, []Interval{a, b})
			for i, v := range got {
				if v.Start > v.End || v.Start < -3 || v.End > 3 {
					t.Fatalf("invalid result %v for %v", got, input)
				}
				if i > 0 && got[i-1].End+1 >= v.Start {
					t.Fatalf("not canonical: %v for %v", got, input)
				}
			}
			for x := -4; x <= 4; x++ {
				want := (a.Start <= x && x <= a.End) || (b.Start <= x && x <= b.End)
				actual := false
				for _, v := range got {
					actual = actual || (v.Start <= x && x <= v.End)
				}
				if actual != want {
					t.Fatalf("coverage x=%d input=%v output=%v", x, input, got)
				}
			}
			again, err := Normalize(got)
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatalf("not idempotent: %v -> %v (%v)", got, again, err)
			}
		}
	}
}
