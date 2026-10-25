// The widths Driga is reviewed at, shared by the toolbar and the catalog
// check: the desktop app's usual window, a small laptop window, and the
// Chat column, where Chat's cards and panels must still read.
export const reviewWidths = {
  desktop: { width: 1440, height: 900 },
  laptop: { width: 1024, height: 768 },
  chat: { width: 400, height: 900 },
};

export const viewportOptions = Object.fromEntries(
  Object.entries(reviewWidths).map(([name, { width, height }]) => [name, { name, styles: { width: `${width}px`, height: `${height}px` } }]),
);
