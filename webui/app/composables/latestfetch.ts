// omega webui: the refetch pipeline's latest-wins guard. FilesTab's loadRoot races
// itself at boot: the mount fetch goes out while focus is still unresolved (the server
// answers the default/cwd fallback), the focus watch's re-root refetch goes out
// milliseconds later - and the STALE response can resolve after the fresh one's,
// clobbering the header + tree (the tab sat wrong until a remount).
export type RoundAdmit = () => boolean;

/**
 * every begin revokes the previous round.
 * The predicate is also the caller's settle signal (loading flags, error slots ride the
 * same verdict).
 */
export interface Rounds {
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