package mysqlprotocol

import (
	"fmt"
	"lib/logger"
)

func (r *MySQLResponse) cutOne() {
	if len(r.Data) > 0 {
		r.Data = r.Data[1:]
	}
	r.Cursor = 0
}

func (r *MySQLResponse) parseNormal() error {
	if len(r.Data) <= 4 {
		return errPacketTooShort
	}
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	pktSeq, c := parseInt1(d, c)
	logger.Trace.Printf("parseNormal~Data:% x,Cursor:%d,pktLen:%d,pktSeq:%d\n", d, c, pktLen, pktSeq)
	cmd, c := parseInt1(d, c)
	if cmd == 0x00 {
		return r.parseOKPacket()
	} else if cmd == 0xFF {
		return r.parseERRPacket()
	} else if cmd == 0xFE {
		if pktLen == 5 {
			return r.parseEOFPacket()
		}
		return r.parseOKPacket()
	}
	return r.parseResultSet()
}

func (r *MySQLResponse) parseOKPacket() error {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	pktSeq, c := parseInt1(d, c)
	logger.Trace.Printf("parseOKPacket~Data:% x,Cursor:%d,pktLen:%d,pktSeq:%d\n", d, c, pktLen, pktSeq)
	mark, c := parseInt1(d, c)
	if mark != 0x00 && mark != 0xFE {
		// (0xFE if CLIENT_DEPRECATE_EOF is set)
		return errPacketNotParse
	}
	affectedRows, c := parseIntLenenc(d, c)
	lastInsertID, c := parseIntLenenc(d, c)
	serverStatus, c := parseInt2(d, c)
	if _, ok := serverStatusFlag[serverStatus]; !ok {
		return errPacketNotParse
	}
	warningCount, c := parseInt2(d, c)
	// if session_tracking_supported (see CLIENT_SESSION_TRACK)
	//   string<lenenc> info
	//   if (status flags & SERVER_SESSION_STATE_CHANGED)
	//     string<lenenc> session state info
	//     string<lenenc> value of variable
	logger.Info.Printf("OKPacket~affectedRows:%d,lastInsertID:%d,serverStatus:%s,warningCount:%d\n",
		affectedRows, lastInsertID, serverStatusFlag[serverStatus], warningCount)
	r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
	r.Cursor = 0
	if len(r.Data) == 0 {
		return errPacketParsed
	}
	return errPacketNotError
}

func (r *MySQLResponse) parseERRPacket() error {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	pktSeq, c := parseInt1(d, c)
	logger.Trace.Printf("parseERRPacket~Data:% x,Cursor:%d,pktLen:%d,pktSeq:%d\n", d, c, pktLen, pktSeq)
	mark, c := parseInt1(d, c)
	if mark != 0xFF {
		return errPacketNotParse
	}
	errorCode, c := parseInt2(d, c)
	if errorCode == 0xFFFF {
		stage, c := parseInt1(d, c)
		maxStage, c := parseInt1(d, c)
		progress, c := parseInt3(d, c)
		progressInfo, c := parseStringLenenc(d, c)
		off := int(c - r.Cursor - uint64(pktLen))
		if off == 0 {
			r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
			r.Cursor = 0
			logger.Info.Printf("ERRPacket~stage:%d,maxStage:%d,progress:%d,progressInfo:%s,off:%d\n",
				stage, maxStage, progress, progressInfo, off)
			if len(r.Data) == 0 {
				return errPacketParsed
			}
			return errPacketNotError
		} else if off < 0 {
			return errPacketTooShort
		} else if off > 0 {
			return errPacketNotParse
		}
	} else {
		if d[c] == '#' {
			mark, c := parseString(d, c, 1)
			fmt.Println("mark:", mark)
			sqlState, c := parseString(d, c, 5)
			fmt.Println("sqlState:", sqlState)
			r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
			r.Cursor = 0
			logger.Info.Printf("ERRPacket~mark:%s,sqlState:%s\n", mark, sqlState)
			if len(r.Data) == 0 {
				return errPacketParsed
			}
			return errPacketNotError
		}
		r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
		r.Cursor = 0
		if len(r.Data) == 0 {
			return errPacketParsed
		}
		return errPacketNotError
	}
	if len(r.Data) == 0 {
		return errPacketParsed
	}
	return errPacketNotError
}

func (r *MySQLResponse) parseEOFPacket() error {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	pktSeq, c := parseInt1(d, c)
	logger.Trace.Printf("parseEOFPacket~Data:% x,Cursor:%d,pktLen:%d,pktSeq:%d\n", d, c, pktLen, pktSeq)
	mark, c := parseInt1(d, c)
	if mark != 0xFE {
		return errPacketNotParse
	}
	warningCount, c := parseInt2(d, c)
	serverStatus, c := parseInt2(d, c)
	if _, ok := serverStatusFlag[serverStatus]; !ok {
		return errPacketNotParse
	}
	logger.Info.Printf("EOFPacket~serverStatus:%s,warningCount:%d\n", serverStatusFlag[serverStatus], warningCount)
	r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
	r.Cursor = 0
	if len(r.Data) == 0 {
		return errPacketParsed
	}
	return errPacketNotError
}

func (r *MySQLResponse) parseResultSet() error {
	count, err := r._parseColumnCountPacket()
	if err != nil {
		return errPacketNotParse
	}
	for i := uint64(0); i < count; i++ {
		r._parseColumnDefinitionPacket()
	}
	r._parseTextResultsetRow(count)
	return errPacketParsed
}

func (r *MySQLResponse) _parseColumnCountPacket() (uint64, error) {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	pktSeq, c := parseInt1(d, c)
	logger.Trace.Printf("_parseColumnCountPacket~Data:% x,Cursor:%d,pktLen:%d,pktSeq:%d\n", d, c, pktLen, pktSeq)
	if pktLen != 0 {
		return 0, errPacketNotParse
	}
	count, c := parseIntLenenc(d, c)
	logger.Info.Printf("_parseColumnCountPacket~count:%d\n", count)
	r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
	r.Cursor = 0
	return count, nil
}

func (r *MySQLResponse) _parseColumnDefinitionPacket() error {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	pktSeq, c := parseInt1(d, c)
	logger.Trace.Printf("_parseColumnDefinitionPacket~Data:% x,Cursor:%d,pktLen:%d,pktSeq:%d\n", d, c, pktLen, pktSeq)
	catalog, c := parseStringLenenc(d, c)
	schema, c := parseStringLenenc(d, c)
	tableAlias, c := parseStringLenenc(d, c)
	table, c := parseStringLenenc(d, c)
	columnAlias, c := parseStringLenenc(d, c)
	column, c := parseStringLenenc(d, c)
	lengthOfFixedFields, c := parseIntLenenc(d, c)
	characterSetNumber, c := parseInt2(d, c)
	maxColumnSize, c := parseInt4(d, c)
	fieldTypes, c := parseInt1(d, c)
	if _, ok := resultSetFieldTypes[fieldTypes]; !ok {
		logger.Trace.Printf("fieldTypes not found: %d\n", fieldTypes)
		return errPacketNotParse
	}
	fieldDetailFlag, c := parseInt2(d, c)
	if _, ok := resultSetFieldDetailFlag[fieldDetailFlag]; !ok {
		logger.Trace.Printf("fieldDetailFlag not found: %d\n", fieldDetailFlag)
		// TODO: why not parse
		// return errPacketNotParse
	}
	decimals, c := parseInt1(d, c)
	unused, c := parseInt2(d, c)
	logger.Info.Printf("_parseColumnDefinitionPacket~catalog:%s,schema:%s,tableAlias:%s,table:%s,columnAlias:%s,column:%s\n",
		catalog, schema, tableAlias, table, columnAlias, column)
	logger.Info.Printf("_parseColumnDefinitionPacket~lengthOfFixedFields:%d,characterSetNumber:%d,maxColumnSize:%d,"+
		"fieldTypes:%s,fieldDetailFlag:%s,decimals:%d,unused:%d\n",
		lengthOfFixedFields, characterSetNumber, maxColumnSize, resultSetFieldTypes[fieldTypes], resultSetFieldDetailFlag[fieldDetailFlag],
		decimals, unused)
	r.Data = r.Data[r.Cursor+4+uint64(pktLen):]
	r.Cursor = 0
	return nil
}

func (r *MySQLResponse) _parseTextResultsetRow(count uint64) error {
	r.parseEOFPacket()
	for {
		if r.isEOFPacket() {
			r.parseEOFPacket()
			break
		}
		d, c := r.Data, r.Cursor
		_, c = parseInt3(d, c)
		_, c = parseInt1(d, c)
		for i := uint64(0); i < count; i++ {
			var tmp string
			tmp, c = parseStringLenenc(d, c)
			logger.Info.Printf("_parseTextResultsetRow~data:%s", tmp)
		}
	}
	return nil
}

func (r *MySQLResponse) isOKPacket() bool {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	if uint64(pktLen) > uint64(len(r.Data)-4) {
		return false
	}
	d = r.Data[r.Cursor+4 : r.Cursor+4+uint64(pktLen)]
	if d[0] == 0x00 || (d[0] == 0xFE && !r.isEOFPacket()) {
		return true
	}
	return false
}

func (r *MySQLResponse) isERRPacket() {

}

func (r *MySQLResponse) isEOFPacket() bool {
	d, c := r.Data, r.Cursor
	pktLen, c := parseInt3(d, c)
	if uint64(pktLen) > uint64(len(r.Data)-4) {
		return false
	}
	d = r.Data[r.Cursor : r.Cursor+4+uint64(pktLen)]
	if pktLen == 5 && d[0] == 0xFE {
		return true
	}
	return false
}

func (r *MySQLResponse) parseInitialHandshakePacket() error {
	return nil
}
