package source

import "testing"

func TestCommonsCategoryFilters(t *testing.T) {
	valid := CommonsSearch{Categories: []string{"Nature", "Night photography"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	legacy := CommonsSearch{Category: " Landscapes of England "}
	if err := legacy.Validate(); err != nil || len(legacy.CategoryNames()) != 1 || legacy.CategoryNames()[0] != "Landscapes of England" {
		t.Fatalf("legacy category was rejected: %v, %v", legacy.CategoryNames(), err)
	}
	for _, invalid := range []CommonsSearch{
		{Categories: []string{"Category:Night"}},
		{Categories: []string{"Night photography", "night_photography"}},
		{Categories: []string{"Night photo\" filew:7000"}},
		{Category: "Nature", Categories: []string{"Night"}},
		{Categories: []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid Commons filter was accepted: %+v", invalid)
		}
	}
}
