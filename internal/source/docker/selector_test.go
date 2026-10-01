package docker

import "testing"

func TestParseSelector(t *testing.T) {
	tests := []struct {
		raw     string
		want    Selector
		wantErr bool
	}{
		{raw: "", want: Selector{mode: modeOwnProject}},
		{raw: "compose", want: Selector{mode: modeOwnProject}},
		{raw: "project=shop", want: Selector{mode: modeProject, value: "shop"}},
		{raw: "label=role=api", want: Selector{mode: modeLabel, value: "role=api"}},
		{raw: "name=web-1", want: Selector{mode: modeName, value: "web-1"}},
		{raw: "service=web", want: Selector{mode: modeService, value: "web"}},
		{raw: "bogus=1", wantErr: true},
		{raw: "project=", wantErr: true},
		{raw: "nope", wantErr: true},
		{raw: "label=role", wantErr: true},
	}

	for _, tt := range tests {
		got, err := ParseSelector(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseSelector(%q) = %+v, want error", tt.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSelector(%q): %v", tt.raw, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseSelector(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
}

func TestSelectorMatches(t *testing.T) {
	c := Container{
		ID:    "id",
		Name:  "/web-1",
		Names: []string{"/web-1"},
		Labels: map[string]string{
			labelComposeProject: "shop",
			labelComposeService: "web",
			"role":              "api",
		},
	}

	tests := []struct {
		selector Selector
		want     bool
	}{
		{Selector{mode: modeProject, value: "shop"}, true},
		{Selector{mode: modeProject, value: "other"}, false},
		{Selector{mode: modeLabel, value: "role=api"}, true},
		{Selector{mode: modeLabel, value: "role=db"}, false},
		{Selector{mode: modeName, value: "web-1"}, true},
		{Selector{mode: modeName, value: "/web-1"}, true},
		{Selector{mode: modeName, value: "other"}, false},
		{Selector{mode: modeService, value: "web"}, true},
		{Selector{mode: modeService, value: "db"}, false},
	}

	for _, tt := range tests {
		if got := tt.selector.Matches(c); got != tt.want {
			t.Errorf("%s.Matches = %v, want %v", tt.selector, got, tt.want)
		}
	}
}
