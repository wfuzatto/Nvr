package evidence

import (
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/store"
)

type fakeCameras struct{ c model.Camera }
func (f fakeCameras) List() []model.Camera { return []model.Camera{f.c} }
func (f fakeCameras) Get(id string) (model.Camera,error) { if id==f.c.ID { return f.c,nil }; return model.Camera{},store.ErrNotFound }
func (f fakeCameras) Put(model.Camera) error { return nil }
func (f fakeCameras) Delete(string) error { return nil }

func TestCreateRejectsLongRange(t *testing.T) {
	root:=t.TempDir()
	m,err:=NewManager(root,root+"/exports",fakeCameras{c:model.Camera{ID:"cam-1",Name:"Cam"}})
	if err!=nil { t.Fatal(err) }
	_,err=m.Create("cam-1",time.Now(),time.Now().Add(5*time.Hour),"tester")
	if err==nil { t.Fatal("expected range limit error") }
}
