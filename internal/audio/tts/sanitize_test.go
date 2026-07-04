package tts

import "testing"

func TestSanitizeTextForSpeech(t *testing.T) {
	got := SanitizeText("## 标题\n- **学校**：[官网](https://example.com)\n| --- | --- |\n`code`")
	want := "标题 学校：官网 code"
	if got != want {
		t.Fatalf("SanitizeText() = %q, want %q", got, want)
	}
}

func TestHasSpeakableText(t *testing.T) {
	if HasSpeakableText("😅 -- ...") {
		t.Fatal("emoji and punctuation should not be speakable")
	}
	if !HasSpeakableText("真的假的呀？") {
		t.Fatal("Chinese text should be speakable")
	}
}
