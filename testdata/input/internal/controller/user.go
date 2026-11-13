package controller

import (
	"example.com/proj/internal/api"
	"example.com/proj/internal/db"
)

func toTime(value string) string {
	return "time:" + value
}

func normalizeName(value string) string {
	return "name:" + value
}

type Local struct{}

//convgen: -s db.Base -d api.Base -fn apiBaseFromDB
//convgen: -s api.Base -d db.Base -fn dbBaseFromAPI -type ptr_ptr
//convgen: -s db.User -d api.User -fn apiUserFull
//    -map DisplayName=Name, Avatar=Profile.Avatar
//    -conv CreatedAt=toTime, UpdatedAt=toTime
//    -deep Tags, Metadata, Ptr
//    -ignore InternalID
//convgen: -s db.Rich -d api.Rich -fn apiUserRich
//    -map DisplayName=Name
//    -conv DisplayName=normalizeName
//    -type ptr_val

var _ = api.Base{}
var _ = db.Base{}
