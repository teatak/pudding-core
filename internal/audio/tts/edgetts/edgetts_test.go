package edgetts

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/audio/tts"
)

func TestSpeedToRate(t *testing.T) {
	tests := map[float32]string{
		0:   "+0%",
		1:   "+0%",
		1.2: "+20%",
		0.9: "-10%",
	}
	for speed, want := range tests {
		if got := speedToRate(speed); got != want {
			t.Fatalf("speedToRate(%v)=%q want %q", speed, got, want)
		}
	}
}

func TestSSMLEscapesTextAndUsesVoice(t *testing.T) {
	client, err := New(Config{Voice: "zh-CN-YunxiaNeural", Speed: 1.2})
	if err != nil {
		t.Fatal(err)
	}
	ssml := client.ssmlFor(`Tom & "Jerry" <go>`)
	for _, want := range []string{
		`voice name='zh-CN-YunxiaNeural'`,
		`rate='+20%'`,
		`Tom &amp; &quot;Jerry&quot; &lt;go&gt;`,
	} {
		if !strings.Contains(ssml, want) {
			t.Fatalf("ssml missing %q: %s", want, ssml)
		}
	}
}

func TestParseBinaryFrame(t *testing.T) {
	header := []byte("Path:audio\r\n\r\n")
	body := []byte{1, 2, 3, 4}
	payload := make([]byte, 2, 2+len(header)+len(body))
	binary.BigEndian.PutUint16(payload, uint16(len(header)))
	payload = append(payload, header...)
	payload = append(payload, body...)

	got, err := parseBinaryFrame(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("body=%v want %v", got, body)
	}
}

func TestParseTextFramePath(t *testing.T) {
	got := parseTextFramePath([]byte("X-RequestId:abc\r\nPath:turn.end\r\n\r\n{}"))
	if got != "turn.end" {
		t.Fatalf("path=%q want turn.end", got)
	}
}

func TestSpeakBeforeStartErrors(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Speak(nil, tts.Request{Text: "hello"}); err == nil {
		t.Fatal("expected not started error")
	}
}
