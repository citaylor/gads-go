/* The limits of a GADS Lua state (lua.go): its allocator's user data */
#ifndef GADS_LIMITS_H
#define GADS_LIMITS_H

#include <stddef.h>
#include "lua.h"

/* The memory in use and its limit, and the instructions run by the current
 * call and their limit (a limit of 0 is none) */
typedef struct {
	size_t used, limit;
	long long steps, maxsteps;
} gads_limits;

/* The pattern matcher of lstrlib.c (changed for GADS) calls
 * gads_match_steps every GADS_MATCH_COUNT steps, which count as
 * instructions: one match can otherwise backtrack without end in C, where
 * the count hook never runs */
#define GADS_MATCH_COUNT 10000
void gads_match_steps(lua_State *L);

#endif
