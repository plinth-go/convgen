package db

type Profile struct {
	Avatar string
}

type Base struct {
	ID   int
	Name string
	Age  int
}

type User struct {
	ID         int
	Name       string
	Age        int
	Profile    Profile
	CreatedAt  string
	UpdatedAt  string
	Tags       []string
	Metadata   map[string]string
	Ptr        *int
	InternalID int
}

type Rich struct {
	Name string
}

type Hidden struct {
	Name     string
	internal int
}

type PointerSource struct {
	Profile *Profile
}

type CountSource struct {
	Count int
}

type SecretSource struct {
	Value  string
	secret string
}

type BadSource struct {
	Count int
}

type Box[T any] struct {
	Value T
}
