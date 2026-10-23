// Run with a Playwright page and the rendered Chat template, app CSS and Chat
// CSS. Embed the shipped fonts in the CSS so font metrics match production.
async function runChatLayoutTests(page, fixture) {
  await page.route("**/*", route => route.abort());
  const results = [];
  for (const height of [600, 650, 667, 700, 812]) {
    await page.setViewportSize({width: 375, height});
    await page.setContent(`<style>${fixture.appCSS}\n${fixture.chatCSS}</style><div class="c chat-page chat-open">${fixture.html}</div>`);
    await page.locator("#chat-history").evaluate(e => e.innerHTML = "<option>Driga · 10/23/2026, 5:00:00 AM</option>");
    await page.locator("#chat-context").evaluate(e => e.textContent = "Context  page: /chat ×  machine: local ×");
    await page.locator("#chat-status").evaluate(e => e.textContent = "Ready");
    await page.evaluate(() => document.fonts.ready);
    const result = await page.evaluate(() => {
      const panel = document.querySelector("#chat").getBoundingClientRect();
      const form = document.querySelector("#chat-form").getBoundingClientRect();
      const hint = document.querySelector("#chat-form > .chat-note").getBoundingClientRect();
      const send = document.querySelector("#chat-send").getBoundingClientRect();
      return {height: innerHeight, panelBottom: panel.bottom, composerBottom: form.bottom,
        hintBottom: hint.bottom, sendBottom: send.bottom};
    });
    if (Math.max(result.composerBottom, result.hintBottom, result.sendBottom) > result.panelBottom + 1) {
      throw new Error("Chat composer clipped: " + JSON.stringify(result));
    }
    results.push(result);
  }
  return results;
}
