export type Media = Record<string, { webm?: string; mp4?: string }>;

/** The local renders while developing, so a retake shows before it is
 * published; otherwise the published sources, if both formats exist. */
export function mediaSources(manifest: Media, film: string, dev: boolean): { webm: string; mp4: string } | undefined {
  if (dev) return { webm: `/media-local/${film}.webm`, mp4: `/media-local/${film}.mp4` };
  const clip = manifest[film];
  return clip?.webm && clip.mp4 ? { webm: clip.webm, mp4: clip.mp4 } : undefined;
}
