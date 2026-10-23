package web

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
)

func webPrompt(t *testing.T, b *chat.Broker) <-chan chat.PromptDecision {
	t.Helper()
	done := make(chan chat.PromptDecision, 1)
	go func() {
		p, _ := b.WaitPrompt(context.Background(), chat.Prompt{ID: "prompt", Session: "session", Cwd: "/repo", Tool: "Bash", Input: json.RawMessage(`{"command":"<script>private</script>"}`)})
		done <- p
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ps, _ := b.Prompts(context.Background())
		if len(ps) > 0 {
			return done
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no prompt")
	return done
}
func TestInboxPromptDecisionsAndAuth(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{true: "allow", false: "deny"}[allow], func(t *testing.T) {
			s, b, _ := chatFixture(t)
			done := webPrompt(t, b)
			page := request(s, "GET", "/inbox", s.token)
			for _, want := range []string{`data-prompt="prompt"`, `data-prompt-answer="allow"`, `data-prompt-answer="deny"`, "&lt;script&gt;private&lt;/script&gt;"} {
				if !strings.Contains(page.Body.String(), want) {
					t.Fatal("missing prompt content", want, page.Body.String())
				}
			}
			if strings.Contains(page.Body.String(), "<script>private") {
				t.Fatal("input not escaped")
			}
			body := `{"id":"prompt","allow":false}`
			if allow {
				body = `{"id":"prompt","allow":true}`
			}
			for _, tc := range []struct {
				origin, csrf, token string
				code                int
			}{
				{"http://127.0.0.1:4321", s.csrf, "", 401},
				{"https://evil.test", s.csrf, s.token, 403},
				{"", s.csrf, s.token, 403},
				{"http://127.0.0.1:4321", "wrong", s.token, 403},
			} {
				w := postChat(s, "/chat/api/prompt-answer", body, tc.origin, tc.csrf, tc.token)
				if w.Code != tc.code {
					t.Fatal(w.Code, w.Body.String())
				}
				ps, _ := b.Prompts(context.Background())
				if len(ps) != 1 {
					t.Fatal("unauthorized answer")
				}
			}
			if w := request(s, "GET", "/chat/api/prompt-answer", s.token); w.Code != 405 {
				t.Fatal(w.Code)
			}
			w := postChat(s, "/chat/api/prompt-answer", body, "http://127.0.0.1:4321", s.csrf, s.token)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			select {
			case p := <-done:
				if (p.Behavior == "allow") != allow {
					t.Fatal(p)
				}
			case <-time.After(time.Second):
				t.Fatal("not answered")
			}
			w = postChat(s, "/chat/api/prompt-answer", body, "http://127.0.0.1:4321", s.csrf, s.token)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "prompt no longer waiting") {
				t.Fatal(w.Code, w.Body.String())
			}
			if page := request(s, "GET", "/inbox", s.token); strings.Contains(page.Body.String(), `data-prompt="prompt"`) {
				t.Fatal("retained prompt")
			}
		})
	}
}
func TestPromptDOM(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	js, err := assets.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	harness := `
const assert=require("node:assert/strict");
(async()=>{
 for(const allow of [true,false])for(const ok of [true,false]){
  const buttons=[{disabled:false},{disabled:false}],status={textContent:""};let removed=false,call;
  const row={querySelector:()=>status,querySelectorAll:()=>buttons,remove(){removed=true;}};
  const button={closest:()=>row,dataset:{promptId:"p",promptAnswer:allow?"allow":"deny"}};
  global.document={querySelector:()=>({content:"csrf"})};
  global.fetch=async(path,options)=>{call={path,options};assert.equal(buttons[0].disabled,true);return {ok,text:async()=>"prompt no longer waiting"};};
  await DrigaConsole.answerPrompt(button);
  assert.equal(call.path,"/chat/api/prompt-answer");assert.equal(call.options.method,"POST");assert.equal(call.options.headers["X-CSRF-Token"],"csrf");assert.equal(call.options.credentials,"same-origin");assert.deepEqual(JSON.parse(call.options.body),{id:"p",allow});
  assert.equal(removed,ok);if(!ok){assert.equal(status.textContent,"prompt no longer waiting");assert.equal(buttons[0].disabled,false);}
 }
})().catch(error=>{console.error(error);process.exitCode=1;});`
	cmd := exec.Command(node, "-")
	cmd.Stdin = strings.NewReader(string(js) + "\n" + harness)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prompt DOM: %v: %s", err, out)
	}
}
