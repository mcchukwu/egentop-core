package org

var RoleHierarchy = map[Role]int{
	RoleMember: 1,
	RoleAdmin:  2,
	RoleOwner:  3,
	RoleViewer: 4,
}
