// The insights TAB's initial window: 7 days (Endo 2026-10-29 request).
// Not the server default (30) a bare /insights gets - the tab opens on
// the 7-day window; the user's pick (30/90/0) wins from there on.
// Its own module so the node suite can assert the default without
// mounting the SFC (the webui test house has no vue-mount infra).
export const INSIGHTS_DEFAULT_DAYS = 7;