// Code generated by "stringer -type=VectorRoles"; DO NOT EDIT.

package gpu

import (
	"errors"
	"strconv"
)

var _ = errors.New("dummy error")

const _VectorRoles_name = "UndefRoleVertexPositionVertexNormalVertexTangentVertexColorVertexTexcoordVertexTexcoord2SkinWeightSkinIndexVectorRolesN"

var _VectorRoles_index = [...]uint8{0, 9, 23, 35, 48, 59, 73, 88, 98, 107, 119}

func (i VectorRoles) String() string {
	if i < 0 || i >= VectorRoles(len(_VectorRoles_index)-1) {
		return "VectorRoles(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _VectorRoles_name[_VectorRoles_index[i]:_VectorRoles_index[i+1]]
}

func (i *VectorRoles) FromString(s string) error {
	for j := 0; j < len(_VectorRoles_index)-1; j++ {
		if s == _VectorRoles_name[_VectorRoles_index[j]:_VectorRoles_index[j+1]] {
			*i = VectorRoles(j)
			return nil
		}
	}
	return errors.New("String: " + s + " is not a valid option for type: VectorRoles")
}
