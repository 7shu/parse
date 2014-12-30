package parse

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestACLMarshal(t *testing.T) {
	acl := NewACL()

	acl.SetPublicReadAccess(true)

	acl.SetReadAccess("abc", true)
	acl.SetReadAccess("def", false)
	acl.SetReadAccess("ghi", true)

	acl.SetWriteAccess("def", false)
	acl.SetWriteAccess("ghi", true)
	acl.SetWriteAccess("jkl", true)

	acl.SetRoleReadAccess("zyx", true)
	acl.SetRoleReadAccess("wvu", false)
	acl.SetRoleReadAccess("tsr", true)

	acl.SetRoleWriteAccess("wvu", false)
	acl.SetRoleWriteAccess("tsr", true)
	acl.SetRoleWriteAccess("qpo", true)

	expected := map[string]map[string]bool{
		"*": map[string]bool{
			"read": true,
		},
		"abc": map[string]bool{
			"read": true,
		},
		"ghi": map[string]bool{
			"read":  true,
			"write": true,
		},
		"jkl": map[string]bool{
			"write": true,
		},
		"role:zyx": map[string]bool{
			"read": true,
		},
		"role:tsr": map[string]bool{
			"read":  true,
			"write": true,
		},
		"role:qpo": map[string]bool{
			"write": true,
		},
	}

	b, err := json.Marshal(acl)
	if err != nil {
		t.Errorf("unexpected error marshaling ACL: %v\n", err)
		t.FailNow()
	}

	actual := map[string]map[string]bool{}
	if err := json.Unmarshal(b, &actual); err != nil {
		t.Errorf("unexpected error unmarshaling ACL: %v\n", err)
		t.FailNow()
	}

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("ACL did not marshal correct.\nGot:\n%v\nExpected:\n%v\n", actual, expected)
	}
}

func TestACLUnmarshal(t *testing.T) {
	b := `{"*":{"read":true},"abc":{"read":true},"def":{"read":true,"write":true},"role:xyz":{"read":true},"role:qrs":{"write":true,"read":true}}`

	acl := NewACL()
	if err := json.Unmarshal([]byte(b), &acl); err != nil {
		t.Errorf("unexpected error unmarshaling acl: %v\n", err)
		t.FailNow()
	}

	if !acl.PublicReadAccess() {
		t.Errorf("ACL does not have public read = true!")
	}

	if acl.PublicWriteAccess() {
		t.Errorf("ACL does has public write = true!")
	}

	cases := []struct {
		key           string
		isRole        bool
		expectedRead  bool
		expectedWrite bool
	}{
		{"abc", false, true, false},
		{"def", false, true, true},
		{"xyz", true, true, false},
		{"qrs", true, true, true},
		{"ghi", false, false, false},
		{"123", false, false, false},
		{"aaa", true, false, false},
		{"bbb", true, false, false},
	}

	for _, c := range cases {
		if c.isRole {
			if acl.RoleReadAccess(c.key) != c.expectedRead {
				t.Errorf("acl did not unmarshal correctly. Expected read=%v for role [%v], got %v\n", c.expectedRead, c.key, !c.expectedRead)
			}
			if acl.RoleWriteAccess(c.key) != c.expectedWrite {
				t.Errorf("acl did not unmarshal correctly. Expected write=%v for role [%v], got %v\n", c.expectedWrite, c.key, !c.expectedWrite)
			}
		} else {
			if acl.ReadAccess(c.key) != c.expectedRead {
				t.Errorf("acl did not unmarshal correctly. Expected read=%v for id [%v], got %v\n", c.expectedRead, c.key, !c.expectedRead)
			}
			if acl.WriteAccess(c.key) != c.expectedWrite {
				t.Errorf("acl did not unmarshal correctly. Expected write=%v for id [%v], got %v\n", c.expectedWrite, c.key, !c.expectedWrite)
			}
		}
	}
}
