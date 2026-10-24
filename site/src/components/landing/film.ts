/** Templates keep downloads inert until the film is on screen and motion is
 * allowed. The visible theme's video plays; the other stays paused. */
export function mountFilm(root: HTMLElement) {
  const slots = [...root.querySelectorAll<HTMLElement>("[data-film-theme]")];
  const toggle = root.querySelector<HTMLButtonElement>("[data-film-toggle]");
  const seekers = [...root.querySelectorAll<HTMLButtonElement>("[data-film-seek]")];
  if (!root.querySelector("template") || !toggle) return () => {};
  const motion = matchMedia("(prefers-reduced-motion: reduce)");
  const narrow = matchMedia("(max-width: 39.999rem)");
  let visible = false, paused = false, disposed = false, frame = 0;
  const videos = new Map<string, HTMLVideoElement>();

  const theme = () => (document.documentElement.dataset.theme === "light" ? "light" : "dark");
  function video(slot: HTMLElement) {
    const size = narrow.matches && slot.querySelector('[data-size="narrow"]') ? "narrow" : "wide";
    const key = `${slot.dataset.filmTheme}:${size}`;
    let player = videos.get(key);
    if (!player) {
      const template = slot.querySelector<HTMLTemplateElement>(`template[data-size="${size}"]`);
      if (!template) return undefined;
      player = template.content.firstElementChild!.cloneNode(true) as HTMLVideoElement;
      player.muted = true;
      const current = player;
      current.addEventListener("playing", () => { if (!disposed && !current.paused) slot.dataset.playing = ""; });
      current.addEventListener("error", () => { delete slot.dataset.playing; });
      slot.append(current);
      videos.set(key, current);
    }
    return player;
  }
  const active = () => slots.find((slot) => slot.dataset.filmTheme === theme());

  function update() {
    const allowed = !disposed && visible && !document.hidden && !motion.matches;
    toggle!.hidden = !allowed;
    toggle!.classList.toggle("paused", paused);
    toggle!.setAttribute("aria-label", paused ? "Play video" : "Pause video");
    const current = active();
    const player = current && allowed ? video(current) : undefined;
    for (const other of videos.values()) if (other !== player) other.pause();
    for (const slot of slots) if (slot !== current || !player) delete slot.dataset.playing;
    if (player) {
      // A replaced narrow/wide video leaves the slot's other one hidden.
      for (const el of current!.querySelectorAll("video")) el.style.visibility = el === player ? "" : "hidden";
      if (paused) player.pause();
      else void player.play().catch(() => { paused = true; update(); });
    }
  }

  // Chapters follow playback and seek on click.
  function progress() {
    const player = [...videos.values()].find((v) => !v.paused) ?? [...videos.values()][0];
    if (player) {
      seekers.forEach((button, i) => {
        const start = Number(button.dataset.filmSeek);
        const end = Number(seekers[i + 1]?.dataset.filmSeek ?? player.duration);
        const done = Math.min(1, Math.max(0, (player.currentTime - start) / (end - start || 1)));
        button.querySelector<HTMLElement>(".dg-film-progress")!.style.setProperty("--progress", String(done));
        button.setAttribute("aria-current", String(player.currentTime >= start && player.currentTime < end));
      });
    }
    frame = requestAnimationFrame(progress);
  }
  const seek = (event: Event) => {
    const start = Number((event.currentTarget as HTMLElement).dataset.filmSeek);
    const current = active();
    const player = current && video(current);
    if (!player) return;
    player.currentTime = start;
    paused = false;
    update();
  };

  const intersection = new IntersectionObserver((entries) => {
    visible = entries.some((entry) => entry.isIntersecting);
    update();
  }, { threshold: 0.25 });
  const themes = new MutationObserver(update);
  const flip = () => { paused = !paused; update(); };
  intersection.observe(root);
  themes.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  toggle.addEventListener("click", flip);
  seekers.forEach((button) => button.addEventListener("click", seek));
  motion.addEventListener("change", update);
  narrow.addEventListener("change", update);
  document.addEventListener("visibilitychange", update);
  if (seekers.length) frame = requestAnimationFrame(progress);
  return () => {
    disposed = true;
    cancelAnimationFrame(frame);
    update();
    intersection.disconnect();
    themes.disconnect();
    toggle.removeEventListener("click", flip);
    seekers.forEach((button) => button.removeEventListener("click", seek));
    motion.removeEventListener("change", update);
    narrow.removeEventListener("change", update);
    document.removeEventListener("visibilitychange", update);
  };
}
