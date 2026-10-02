package form

import "testing"

func TestFieldValidation(t *testing.T) {
	for _, tc := range []struct {
		field Field
		value string
		valid bool
	}{
		{Field{Required: true, Kind: Text}, "   ", false},
		{Field{Kind: Number}, "3", true}, {Field{Kind: Number}, "3.5", false},
		{Field{Kind: Boolean}, "true", true}, {Field{Kind: Boolean}, "maybe", false},
		{Field{Kind: Choice, Options: []string{"a", "b"}}, "b", true}, {Field{Kind: Choice, Options: []string{"a", "b"}}, "c", false},
	} {
		if err := tc.field.Validate(tc.value); (err == nil) != tc.valid {
			t.Fatalf("field=%+v value=%q err=%v", tc.field, tc.value, err)
		}
	}
}
