// omega webui: the refetch pipeline's latest-wins guard. FilesTab's loadRoot races
// itself at boot, and the STALE response can resolve last, clobbering the header + tree.

export type RoundAdmit = () => boolean;

/** every begin revokes the previous round; the predicate is the caller's settle signal */
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