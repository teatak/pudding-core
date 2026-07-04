package api

import "net/http"

const browserTestFormPath = "/browser-test-form"

const browserTestFormHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Pudding Browser Test Form</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 720px; margin: 48px auto; padding: 0 20px; line-height: 1.5; }
    form { display: grid; gap: 16px; }
    label { display: grid; gap: 6px; font-weight: 600; }
    input, textarea, button { font: inherit; padding: 10px 12px; border: 1px solid #bbb; border-radius: 8px; }
    textarea { min-height: 120px; }
    button { width: fit-content; cursor: pointer; background: #111; color: white; border-color: #111; }
    #result { margin-top: 24px; padding: 12px; border: 1px solid #ddd; border-radius: 8px; background: #f7f7f7; white-space: pre-wrap; }
    .spacer { height: 900px; }
  </style>
</head>
<body>
  <h1>Pudding Browser Test Form</h1>
  <form id="test-form">
    <label>Name
      <input id="name" name="name" autocomplete="off">
    </label>
    <label>Comments
      <textarea id="comments" name="comments"></textarea>
    </label>
    <button id="save" type="submit">Save</button>
  </form>
  <pre id="result">Waiting</pre>
  <div class="spacer"></div>
  <p id="bottom">Bottom marker</p>
  <script>
    document.getElementById("test-form").addEventListener("submit", function(event) {
      event.preventDefault();
      const data = new FormData(event.currentTarget);
      document.getElementById("result").textContent = JSON.stringify({
        name: data.get("name"),
        comments: data.get("comments")
      }, null, 2);
    });
  </script>
</body>
</html>`

func serveBrowserTestForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(browserTestFormHTML))
}
