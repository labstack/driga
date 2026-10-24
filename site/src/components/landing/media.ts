export type Media = Record<string, { webm?: string; mp4?: string }>;

/** While developing, the local renders if `just site-footage` made them, so a
 * retake shows before it is published; otherwise the published sources, if
 * both formats exist. `local` says whether a local render exists. */
export function mediaSources(manifest: Media, film: string, local: boolean): { webm: string; mp4: string } | undefined {
  if (local) return { webm: `/media-local/${film}.webm`, mp4: `/media-local/${film}.mp4` };
  const clip = manifest[film];
  return clip?.webm && clip.mp4 ? { webm: clip.webm, mp4: clip.mp4 } : undefined;
}
