package audit

import (
	"os"
	"testing"
)

func TestAuditHashChain(t *testing.T) {
	path:=t.TempDir()+"/audit.jsonl"
	log,err:=Open(path)
	if err!=nil { t.Fatal(err) }
	if _,err:=log.Append(Event{Actor:"admin",Action:"camera.create",Success:true}); err!=nil { t.Fatal(err) }
	if _,err:=log.Append(Event{Actor:"admin",Action:"camera.delete",Success:true}); err!=nil { t.Fatal(err) }
	if err:=log.Verify(); err!=nil { t.Fatal(err) }
	items,err:=log.List(10)
	if err!=nil || len(items)!=2 { t.Fatalf("items=%d err=%v",len(items),err) }

	data,err:=os.ReadFile(path)
	if err!=nil { t.Fatal(err) }
	data[len(data)/2]^=1
	if err:=os.WriteFile(path,data,0o600); err!=nil { t.Fatal(err) }
	if err:=log.Verify(); err==nil { t.Fatal("tampering was not detected") }
}
