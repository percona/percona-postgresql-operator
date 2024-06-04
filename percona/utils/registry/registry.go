package registry

import "github.com/pkg/errors"

// Objects defines a list of objects.
type Objects map[string]interface{}

// Registry defines a registry object that can register objects.
type Registry struct {
	objects Objects
}

// New creates a new registry object.
func New() *Registry {
	return &Registry{
		objects: Objects{},
	}
}

// Add adds an object with a given unique id to registry.
func (r *Registry) Add(name string, object interface{}) error {
	if r.IsExist(name) {
		return errors.Errorf("object with %s was already registered", name)
	}

	r.objects[name] = object

	return nil
}

// IsExist returns true if object with given name was registered, otherwise it returns false.
func (r *Registry) IsExist(name string) bool {
	_, ok := r.objects[name]
	return ok
}

// Remove removes registered object.
func (r *Registry) Remove(name string) *Registry {
	if _, ok := r.objects[name]; !ok {
		return r
	}

	delete(r.objects, name)

	return r
}

// Names returns a list of registered object names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.objects))

	for name := range r.objects {
		names = append(names, name)
	}

	return names
}
