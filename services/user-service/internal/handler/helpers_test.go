package handler

// White-box tests for unexported helper functions.
// Uses package handler (not handler_test) to access unexported symbols.

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	pb "banka-backend/proto/user"
)

// ─── nullStrIf ────────────────────────────────────────────────────────────────

func TestNullStrIf(t *testing.T) {
	got := nullStrIf("hello", true)
	assert.Equal(t, sql.NullString{String: "hello", Valid: true}, got)

	got = nullStrIf("hello", false)
	assert.Equal(t, sql.NullString{String: "hello", Valid: false}, got)
}

// ─── fromNullStr ──────────────────────────────────────────────────────────────

func TestFromNullStr(t *testing.T) {
	assert.Equal(t, "world", fromNullStr(sql.NullString{String: "world", Valid: true}))
	assert.Equal(t, "", fromNullStr(sql.NullString{Valid: false}))
}

// ─── genderFromString ─────────────────────────────────────────────────────────

func TestGenderFromString(t *testing.T) {
	tests := []struct {
		input sql.NullString
		want  pb.Gender
	}{
		{sql.NullString{Valid: false}, pb.Gender_GENDER_UNSPECIFIED},
		{sql.NullString{String: "MALE", Valid: true}, pb.Gender_GENDER_MALE},
		{sql.NullString{String: "FEMALE", Valid: true}, pb.Gender_GENDER_FEMALE},
		{sql.NullString{String: "OTHER", Valid: true}, pb.Gender_GENDER_OTHER},
		{sql.NullString{String: "UNKNOWN", Valid: true}, pb.Gender_GENDER_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, genderFromString(tc.input), "input: %v", tc.input)
	}
}

// ─── genderToString ───────────────────────────────────────────────────────────

func TestGenderToString(t *testing.T) {
	tests := []struct {
		input pb.Gender
		want  string
	}{
		{pb.Gender_GENDER_MALE, "MALE"},
		{pb.Gender_GENDER_FEMALE, "FEMALE"},
		{pb.Gender_GENDER_OTHER, "OTHER"},
		{pb.Gender_GENDER_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, genderToString(tc.input), "input: %v", tc.input)
	}
}

// ─── userTypeFromString ───────────────────────────────────────────────────────

func TestUserTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  pb.UserType
	}{
		{"ADMIN", pb.UserType_USER_TYPE_ADMIN},
		{"EMPLOYEE", pb.UserType_USER_TYPE_EMPLOYEE},
		{"CLIENT", pb.UserType_USER_TYPE_CLIENT},
		{"OTHER", pb.UserType_USER_TYPE_UNSPECIFIED},
		{"", pb.UserType_USER_TYPE_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, userTypeFromString(tc.input), "input: %q", tc.input)
	}
}

// ─── userTypeToString ─────────────────────────────────────────────────────────

func TestUserTypeToString(t *testing.T) {
	tests := []struct {
		input pb.UserType
		want  string
	}{
		{pb.UserType_USER_TYPE_ADMIN, "ADMIN"},
		{pb.UserType_USER_TYPE_EMPLOYEE, "EMPLOYEE"},
		{pb.UserType_USER_TYPE_CLIENT, "CLIENT"},
		{pb.UserType_USER_TYPE_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, userTypeToString(tc.input), "input: %v", tc.input)
	}
}

// ─── isValidPhone ─────────────────────────────────────────────────────────────

func TestIsValidPhone(t *testing.T) {
	valid := []string{"", "0641234567", "+381641234567", "123"}
	for _, s := range valid {
		assert.True(t, isValidPhone(s), "should be valid: %q", s)
	}

	invalid := []string{"+", "abc", "+12a3", "06 41"}
	for _, s := range invalid {
		assert.False(t, isValidPhone(s), "should be invalid: %q", s)
	}
}

// ─── isUniqueViolation ────────────────────────────────────────────────────────

func TestIsUniqueViolation(t *testing.T) {
	pgDup := &pgconn.PgError{Code: "23505"}
	assert.True(t, isUniqueViolation(pgDup))

	pgOther := &pgconn.PgError{Code: "42601"}
	assert.False(t, isUniqueViolation(pgOther))

	assert.False(t, isUniqueViolation(errors.New("some other error")))
	assert.False(t, isUniqueViolation(nil))
}
