#include "gads_limits.h"

/* gads_match_steps adds GADS_MATCH_COUNT steps of the pattern matcher to
 * the instructions of the call, and fails as the count hook (lua.go
 * gads_hook) does once they are over the limit */
void gads_match_steps(lua_State *L) {
	void *ud;
	lua_getallocf(L, &ud);
	gads_limits *l = (gads_limits *)ud;
	if (!l->maxsteps) return;
	l->steps += GADS_MATCH_COUNT;
	if (l->steps > l->maxsteps) {
		lua_pushfstring(L, "code stopped after running more than %I instructions", (lua_Integer)l->maxsteps);
		lua_error(L);
	}
}
