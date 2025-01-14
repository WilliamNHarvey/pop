package pop

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/WilliamNHarvey/pop/v6/columns"
	"github.com/gobuffalo/flect"
	nflect "github.com/gobuffalo/flect/name"
	"github.com/gofrs/uuid"
)

var nowFunc = time.Now

// SetNowFunc allows an override of time.Now for customizing CreatedAt/UpdatedAt
func SetNowFunc(f func() time.Time) {
	nowFunc = f
}

// Value is the contents of a `Model`.
type Value interface{}

type modelIterable func(*Model) error

// Model is used throughout Pop to wrap the end user interface
// that is passed in to many functions.
type Model struct {
	Value
	ctx context.Context
	As  string
}

// NewModel returns a new model with the specified value and context.
func NewModel(v Value, ctx context.Context) *Model {
	return &Model{Value: v, ctx: ctx}
}

// ID returns the ID of the Model. All models must have an `ID` field this is
// of type `int`,`int64` or of type `uuid.UUID`.
func (m *Model) ID() interface{} {
	fbn, err := m.fieldByName("ID")
	if err != nil {
		return nil
	}
	if pkt, _ := m.PrimaryKeyType(); pkt == "UUID" {
		return fbn.Interface().(uuid.UUID).String()
	}
	return fbn.Interface()
}

// IDField returns the name of the DB field used for the ID.
// By default, it will return "id".
func (m *Model) IDField() string {
	modelType := reflect.TypeOf(m.Value)

	// remove all indirections
	for modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Array {
		modelType = modelType.Elem()
	}

	if modelType.Kind() == reflect.String {
		return "id"
	}

	field, ok := modelType.FieldByName("ID")
	if !ok {
		return "id"
	}
	dbField := field.Tag.Get("db")
	if dbField == "" {
		return "id"
	}
	return dbField
}

// PrimaryKeyType gives the primary key type of the `Model`.
func (m *Model) PrimaryKeyType() (string, error) {
	fbn, err := m.fieldByName("ID")
	if err != nil {
		return "", fmt.Errorf("model %T is missing required field ID", m.Value)
	}
	return fbn.Type().Name(), nil
}

// UsingAutoIncrement returns true if the model is not opting out of autoincrement
func (m *Model) UsingAutoIncrement() bool {
	tag, err := m.tagForFieldByName("ID", "no_auto_increment")
	// if there is no `no_auto_increment` tag, or tag isn't true, then we default to relying on auto increment
	return err != nil || tag != "true"
}

// TableNameAble interface allows for the customize table mapping
// between a name and the database. For example the value
// `User{}` will automatically map to "users". Implementing `TableNameAble`
// would allow this to change to be changed to whatever you would like.
type TableNameAble interface {
	TableName() string
}

// TableNameAbleWithContext is equal to TableNameAble but will
// be passed the queries' context. Useful in cases where the
// table name depends on e.g.
type TableNameAbleWithContext interface {
	TableName(ctx context.Context) string
}

// TableName returns the corresponding name of the underlying database table
// for a given `Model`. See also `TableNameAble` to change the default name of the table.
func (m *Model) TableName() string {
	if s, ok := m.Value.(string); ok {
		return s
	}

	if n, ok := m.Value.(TableNameAble); ok {
		return n.TableName()
	}

	if n, ok := m.Value.(TableNameAbleWithContext); ok {
		if m.ctx == nil {
			m.ctx = context.TODO()
		}
		return n.TableName(m.ctx)
	}

	return m.typeName(reflect.TypeOf(m.Value))
}

func (m *Model) Columns() columns.Columns {
	return columns.ForStructWithAlias(m.Value, m.TableName(), m.As, columns.IDField{Name: m.IDField(), Writeable: !m.UsingAutoIncrement()})
}

func (m *Model) cacheKey(t reflect.Type) string {
	return t.PkgPath() + "." + t.Name()
}

func (m *Model) typeName(t reflect.Type) (name string) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		el := t.Elem()
		if el.Kind() == reflect.Ptr {
			el = el.Elem()
		}

		// validates if the elem of slice or array implements TableNameAble interface.
		var tableNameAble *TableNameAble
		if el.Implements(reflect.TypeOf(tableNameAble).Elem()) {
			v := reflect.New(el)
			out := v.MethodByName("TableName").Call([]reflect.Value{})
			return out[0].String()
		}

		// validates if the elem of slice or array implements TableNameAbleWithContext interface.
		var tableNameAbleWithContext *TableNameAbleWithContext
		if el.Implements(reflect.TypeOf(tableNameAbleWithContext).Elem()) {
			v := reflect.New(el)
			out := v.MethodByName("TableName").Call([]reflect.Value{reflect.ValueOf(m.ctx)})
			return out[0].String()

			// We do not want to cache contextualized TableNames because that would break
			// the contextualization.
		}
		return nflect.Tableize(el.Name())
	default:
		return nflect.Tableize(t.Name())
	}
}

func (m *Model) fieldByName(s string) (reflect.Value, error) {
	el := reflect.ValueOf(m.Value).Elem()
	fbn := el.FieldByName(s)
	if !fbn.IsValid() {
		return fbn, fmt.Errorf("model does not have a field named %s", s)
	}
	return fbn, nil
}

func (m *Model) tagForFieldByName(fieldName string, tagName string) (string, error) {
	el := reflect.TypeOf(m.Value).Elem()
	if el.Kind() != reflect.Struct {
		return "", fmt.Errorf("model is not a struct")
	}
	fbn, ok := el.FieldByName(fieldName)
	if !ok {
		return "", fmt.Errorf("model does not have a field named %s", fieldName)
	}
	return fbn.Tag.Get(tagName), nil
}

func (m *Model) associationName() string {
	tn := flect.Singularize(m.TableName())
	return fmt.Sprintf("%s_id", tn)
}

func (m *Model) setID(i interface{}) {
	fbn, err := m.fieldByName("ID")
	if err == nil {
		v := reflect.ValueOf(i)
		switch fbn.Kind() {
		case reflect.Int, reflect.Int64:
			fbn.SetInt(v.Int())
		default:
			fbn.Set(reflect.ValueOf(i))
		}
	}
}

func (m *Model) setCreatedAt(now time.Time) {
	fbn, err := m.fieldByName("CreatedAt")
	if err == nil {
		v := fbn.Interface()
		if !IsZeroOfUnderlyingType(v) {
			// Do not override already set CreatedAt
			return
		}
		switch v.(type) {
		case int, int64:
			fbn.SetInt(now.Unix())
		default:
			fbn.Set(reflect.ValueOf(now))
		}
	}
}

func (m *Model) setUpdatedAt(now time.Time) {
	fbn, err := m.fieldByName("UpdatedAt")
	if err == nil {
		v := fbn.Interface()
		switch v.(type) {
		case int, int64:
			fbn.SetInt(now.Unix())
		default:
			fbn.Set(reflect.ValueOf(now))
		}
	}
}

func (m *Model) WhereID() string {
	return fmt.Sprintf("%s.%s = ?", m.Alias(), m.IDField())
}

func (m *Model) Alias() string {
	as := m.As
	if as == "" {
		as = strings.ReplaceAll(m.TableName(), ".", "_")
	}
	return as
}

func (m *Model) WhereNamedID() string {
	return fmt.Sprintf("%s.%s = :%s", m.Alias(), m.IDField(), m.IDField())
}

func (m *Model) isSlice() bool {
	v := reflect.Indirect(reflect.ValueOf(m.Value))
	return v.Kind() == reflect.Slice || v.Kind() == reflect.Array
}

func (m *Model) iterate(fn modelIterable) error {
	if m.isSlice() {
		v := reflect.Indirect(reflect.ValueOf(m.Value))
		for i := 0; i < v.Len(); i++ {
			val := v.Index(i)
			newModel := &Model{
				Value: val.Addr().Interface(),
				ctx:   m.ctx,
			}
			err := fn(newModel)

			if err != nil {
				return err
			}
		}
		return nil
	}

	return fn(m)
}
