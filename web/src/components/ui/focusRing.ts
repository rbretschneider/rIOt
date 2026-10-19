// One focus-visible ring shared by every interactive primitive (Button, Modal
// close, toast close, header clock). Keyboard users get a clear 2px accent ring
// offset from the dark surface; pointer users never see it.
export const focusRing =
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface'
