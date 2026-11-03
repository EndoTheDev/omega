// omega webui: the refetch pipeline's latest-wins guard (card
// t_3e12c367). FilesTab's loadRoot races itself at boot: the mount
// fetch goes out while focus is still unresolved (the server answers
// the default/cwd fallback), the focus watch's re-root refetch goes
// out milliseconds later - and the STALE response can resolve after
// the fresh one's, clobbering the header + tree (the tab sat wrong
// until a remount). Every request claims a round; only the currently
// latest round is admitted to write. Render-free; node's built-in
// test runner.
export type RoundAdmit = () => boolean;

/** every begin() revokes the previous round; the returned predicate
    stays true only while ITS round is still the latest request. The
    predicate is also the caller's settle signal (loading flags, error
    slots ride the same verdict). */
export interface Rounds {
  /** start one round; the admit check for the request about to fly */
  begin: () => RoundAdmit;
}

export function latestWins(): Rounds {
  let seq = 0;
  return {
    begin: () => {
      const mine = ++seq;
      return () => mine === seq;
    },
  };
}