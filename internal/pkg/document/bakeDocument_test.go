package document

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParentTargets(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		target   string
		targets  []string
		resolved bool
	}{
		{
			name: "empty block",
			content: `
target t1 {}`,
			target:   "t1",
			targets:  nil,
			resolved: true,
		},
		{
			name: "inheriting non-existent target",
			content: `
target t1 { inherits = ["t2"]}`,
			target:   "t1",
			targets:  nil,
			resolved: false,
		},
		{
			name: "inheriting valid quoted target",
			content: `
target t1 { inherits = ["t2"]}
target "t2" {}`,
			target:   "t1",
			targets:  []string{"t2"},
			resolved: true,
		},
		{
			name: "inheriting valid target",
			content: `
target t1 { inherits = ["t2"]}
target t2 {}`,
			target:   "t1",
			targets:  []string{"t2"},
			resolved: true,
		},
		{
			name: "two-way recursion",
			content: `
target t1 { inherits = ["t2"]}
target t2 { inherits = ["t1"]}`,
			target:   "t1",
			targets:  nil,
			resolved: false,
		},
		{
			name: "three-way recursion",
			content: `
target t1 { inherits = ["t2"]}
target t2 { inherits = ["t3"]}
target t3 { inherits = ["t1"]}`,
			target:   "t1",
			targets:  nil,
			resolved: false,
		},
		{
			name: "two-level inheritance",
			content: `
target t1 { inherits = ["t2", "t3"]}
target t2 { inherits = ["t4"]}
target t3 { inherits = ["t5"]}
target t4 { }
target t5 { }`,
			target:   "t1",
			targets:  []string{"t5", "t2", "t4", "t3"},
			resolved: true,
		},
		{
			name: "variables are not resolved",
			content: `
target t1 { inherits = ["t2", var]}
target t2 { }`,
			target:   "t1",
			targets:  nil,
			resolved: false,
		},
		{
			name: "${variables} are not resolved",
			content: `
target t1 { inherits = ["t2", "${var}"]}
target t2 { }`,
			target:   "t1",
			targets:  nil,
			resolved: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			doc := NewBakeHCLDocument("file:///tmp/docker-bake.hcl", 1, []byte(tc.content))
			targets, resolved := doc.ParentTargets(tc.target)
			require.Equal(t, tc.targets, targets)
			require.Equal(t, tc.resolved, resolved)
		})
	}
}
