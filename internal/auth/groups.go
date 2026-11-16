package auth

import "sync/atomic"

// Some identity providers send only a group's direct members over SCIM, with
// each group's parents, so membership is counted up the hierarchy here. The
// walks use UNION, which stops at cycles.

var groupParents atomic.Bool

// UseGroupParents makes a group's members count as members of the groups
// above it. Parents come only over SCIM, so the server turns this on while
// SCIM manages groups, and rows left from an earlier SCIM setup count for
// nothing once it doesn't.
func UseGroupParents(on bool) { groupParents.Store(on) }

// MemberOfSQL selects the ids of the groups user u is in.
func MemberOfSQL() string {
	if !groupParents.Load() {
		return `SELECT gm.group_id FROM group_members gm WHERE gm.user_id = u.id`
	}

	// OFFSET 0 keeps each step an index lookup per group: joined, the planner
	// scans every parent link at each level.
	return `WITH RECURSIVE member_of(id) AS (
		SELECT gm.group_id FROM group_members gm WHERE gm.user_id = u.id
		UNION
		SELECT above.id FROM member_of m
		CROSS JOIN LATERAL (
			SELECT parent.id FROM group_parents gp
			JOIN groups parent ON parent.external_id = gp.parent_external_id
			WHERE gp.group_id = m.id OFFSET 0
		) above
	) SELECT id FROM member_of`
}

// EffectiveRoleSQL selects the highest role bound to user u directly or
// through one of their groups; everyone is at least a member.
func EffectiveRoleSQL() string {
	return `coalesce((
		SELECT rb.role FROM role_bindings rb
		WHERE (rb.subject_type = 'user' AND rb.subject_id = u.id)
			OR (rb.subject_type = 'group' AND rb.subject_id IN (` + MemberOfSQL() + `))
		ORDER BY CASE rb.role WHEN 'admin' THEN 3 WHEN 'builder' THEN 2 ELSE 1 END DESC
		LIMIT 1
	), 'member')`
}

// GroupNamesSQL selects the sorted names of the groups user u is in, as a
// text array, so the principal carries them without a second query.
func GroupNamesSQL() string {
	return `coalesce((
		SELECT array_agg(g.name ORDER BY g.name) FROM groups g
		WHERE g.id IN (` + MemberOfSQL() + `)
	), '{}')`
}

// MemberCountsSQL selects (group_id, members): how many people are in each
// group the subquery groupIDs selects, walking down from all of them at
// once. Groups without anyone are left out.
func MemberCountsSQL(groupIDs string) string {
	if !groupParents.Load() {
		return `SELECT member.group_id, count(*) AS members FROM group_members member
			WHERE member.group_id IN (` + groupIDs + `)
			GROUP BY member.group_id`
	}

	return `WITH RECURSIVE below(top_id, id, external_id) AS (
		SELECT top.id, top.id, top.external_id FROM groups top WHERE top.id IN (` + groupIDs + `)
		UNION
		SELECT below.top_id, child.id, child.external_id FROM below
		JOIN group_parents link ON link.parent_external_id = below.external_id
		JOIN groups child ON child.id = link.group_id
	)
	SELECT below.top_id AS group_id, count(DISTINCT member.user_id) AS members FROM below
	JOIN group_members member ON member.group_id = below.id
	GROUP BY below.top_id`
}

// MembershipsSQL selects every (group_id, user_id) pair of a person in a
// group, once, for reading all groups' members at a time.
func MembershipsSQL() string {
	if !groupParents.Load() {
		return `SELECT group_id, user_id FROM group_members`
	}

	return `WITH RECURSIVE closure(group_id, member_group_id) AS (
		SELECT id, id FROM groups
		UNION
		SELECT parent.id, closure.member_group_id FROM closure
		JOIN group_parents link ON link.group_id = closure.group_id
		JOIN groups parent ON parent.external_id = link.parent_external_id
	)
	SELECT DISTINCT closure.group_id, member.user_id FROM closure
	JOIN group_members member ON member.group_id = closure.member_group_id`
}
