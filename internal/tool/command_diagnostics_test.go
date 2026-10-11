package tool

import "testing"

func TestCommandVerificationKindRecognizesStaticInvocations(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		kind string
	}{
		{[]string{"npm", "--prefix", "web", "test"}, "test"},
		{[]string{"npm", "--prefix=web", "run", "build"}, "build"},
		{[]string{"pnpm", "-C", "web", "test"}, "test"},
		{[]string{"yarn", "--cwd", "web", "run", "lint"}, "lint"},
		{[]string{"bun", "--cwd=web", "run", "check"}, "check"},
		{[]string{"npx", "tsc", "--noEmit"}, "check"},
		{[]string{"npx", "--yes", "eslint", "."}, "lint"},
		{[]string{"node", "--test", "test/example.test.js"}, "test"},
		{[]string{"node", "--experimental-strip-types", "--test", "test/example.test.ts"}, "test"},
		{[]string{"npm", "--prefix"}, ""},
		{[]string{"npm", "--unknown", "test"}, ""},
		{[]string{"npm", "run", "test", "--if-present"}, ""},
		{[]string{"npm", "run", "test", "--dry-run"}, ""},
		{[]string{"npm", "--version"}, ""},
		{[]string{"tsc", "--version"}, ""},
		{[]string{"node", "--help", "--test"}, ""},
		{[]string{"node", "script.js", "--test"}, ""},
		{[]string{"node", "-e", "--test"}, ""},
		{[]string{"npx", "--package", "typescript", "tsc"}, ""},
	} {
		if got := commandVerificationKind(tc.argv); got != tc.kind {
			t.Errorf("%q: kind=%q, want %q", tc.argv, got, tc.kind)
		}
	}
}
