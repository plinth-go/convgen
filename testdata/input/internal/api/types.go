package api

type Base struct {
	ID   int
	Name string
	Age  int
}

type User struct {
	ID          int
	Name        string
	Age         int
	DisplayName string
	Avatar      string
	CreatedAt   string
	UpdatedAt   string
	Tags        []string
	Metadata    map[string]string
	Ptr         *int
	InternalID  int
}

type Rich struct {
	Name        string
	DisplayName string
}

type Hidden struct {
	Name     string
	internal int
}

type PointerTarget struct {
	Avatar string
}

type CountTarget struct {
	Count int
}

type SecretTarget struct {
	Value string
}

type BadTarget struct {
	Count string
}
