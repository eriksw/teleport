package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	mssql "github.com/denisenkom/go-mssqldb"
	"github.com/gravitational/trace"
)

// procIDToName maps procID to the special stored procedure name
// https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-tds/619c43b6-9495-4a58-9e49-a4950db245b3
var procIDToName = []string{
	1:  "Sp_Cursor",
	2:  "Sp_CursorOpen",
	3:  "Sp_CursorPrepare",
	4:  "Sp_CursorExecute",
	5:  "Sp_CursorPrepExec",
	6:  "Sp_CursorUnprepare",
	7:  "Sp_CursorFetch",
	8:  "Sp_CursorOption",
	9:  "Sp_CursorClose",
	10: "Sp_ExecuteSql",
	11: "Sp_Prepare",
	12: "Sp_Execute",
	13: "Sp_PrepExec",
	14: "Sp_PrepExecRpc",
	15: "Sp_Unprepare",
}

// RPCRequest defines client RPC Request packet:
// https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-tds/619c43b6-9495-4a58-9e49-a4950db245b3
type RPCRequest struct {
	Packet
	// ProcName contains name of the procedure to be executed.
	ProcName string
	// Parameters contains list of RPC parameters.
	Parameters []string
}

func toRPCRequest(p Packet) (*RPCRequest, error) {
	if p.Type() != PacketTypeRPCRequest {
		return nil, trace.BadParameter("expected SQLBatch packet, got: %#v", p.Type())
	}
	data := p.Data()
	r := bytes.NewReader(p.Data())

	var headersLength uint32
	if err := binary.Read(r, binary.LittleEndian, &headersLength); err != nil {
		return nil, trace.Wrap(err)
	}

	if _, err := r.Seek(int64(headersLength), io.SeekStart); err != nil {
		return nil, trace.ConvertSystemError(err)
	}

	var length uint16
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, trace.Wrap(err)
	}

	if length != 0xFFFF {
		procName, err := readUcs2(r, 2*int(length))
		if err != nil {
			return nil, trace.Wrap(err)
		}
		return &RPCRequest{
			Packet:   p,
			ProcName: procName,
		}, nil
	}

	var procID uint16
	if err := binary.Read(r, binary.LittleEndian, &procID); err != nil {
		return nil, trace.Wrap(err)
	}

	if int(procID) >= len(procIDToName) {
		return nil, trace.BadParameter("invalid procID")
	}

	procName := ""
	if procName = procIDToName[procID]; procName == "" {
		return nil, trace.BadParameter("unmapped procID")
	}

	var flags uint16
	if err := binary.Read(r, binary.LittleEndian, &flags); err != nil {
		return nil, trace.Wrap(err)
	}

	if _, err := r.Seek(2, io.SeekCurrent); err != nil {
		return nil, trace.ConvertSystemError(err)
	}

	tds := mssql.NewTdsBuffer(data[int(r.Size())-r.Len():], r.Len())
	ti := mssql.ReadTypeInfo(tds)
	val := ti.Reader(&ti, tds)

	return &RPCRequest{
		Packet:     p,
		ProcName:   procName,
		Parameters: []string{fmt.Sprintf("%v", val)},
	}, nil
}
