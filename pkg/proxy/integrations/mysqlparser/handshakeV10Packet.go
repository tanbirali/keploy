package mysqlparser

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"go.keploy.io/server/pkg/models"
)

type HandshakeV10Packet struct {
	ProtocolVersion uint8  `yaml:"protocol_version"`
	ServerVersion   string `yaml:"server_version"`
	ConnectionID    uint32 `yaml:"connection_id"`
	AuthPluginData  []byte `yaml:"auth_plugin_data"`
	CapabilityFlags uint32 `yaml:"capability_flags"`
	CharacterSet    uint8  `yaml:"character_set"`
	StatusFlags     uint16 `yaml:"status_flags"`
	AuthPluginName  string `yaml:"auth_plugin_name"`
}

func decodeMySQLHandshakeV10(data []byte) (*HandshakeV10Packet, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("handshake packet too short")
	}

	packet := &HandshakeV10Packet{}
	packet.ProtocolVersion = data[0]

	idx := bytes.IndexByte(data[1:], 0x00)
	if idx == -1 {
		return nil, fmt.Errorf("malformed handshake packet: missing null terminator for ServerVersion")
	}
	packet.ServerVersion = string(data[1 : 1+idx])
	data = data[1+idx+1:]

	if len(data) < 4 {
		return nil, fmt.Errorf("handshake packet too short for ConnectionID")
	}
	packet.ConnectionID = binary.LittleEndian.Uint32(data[:4])
	data = data[4:]

	if len(data) < 9 { // 8 bytes of AuthPluginData + 1 byte filler
		return nil, fmt.Errorf("handshake packet too short for AuthPluginData")
	}
	packet.AuthPluginData = append([]byte{}, data[:8]...)
	data = data[9:] // Skip 8 bytes of AuthPluginData and 1 byte filler

	if len(data) < 5 { // Capability flags (2 bytes), character set (1 byte), status flags (2 bytes)
		return nil, fmt.Errorf("handshake packet too short for flags")
	}
	capabilityFlagsLower := binary.LittleEndian.Uint16(data[:2])
	data = data[2:]

	packet.CharacterSet = data[0]
	data = data[1:]

	packet.StatusFlags = binary.LittleEndian.Uint16(data[:2])
	data = data[2:]

	capabilityFlagsUpper := binary.LittleEndian.Uint16(data[:2])
	data = data[2:]

	packet.CapabilityFlags = uint32(capabilityFlagsLower) | uint32(capabilityFlagsUpper)<<16

	if packet.CapabilityFlags&0x800000 != 0 {
		if len(data) < 11 { // AuthPluginDataLen (1 byte) + Reserved (10 bytes)
			return nil, fmt.Errorf("handshake packet too short for AuthPluginDataLen")
		}
		authPluginDataLen := int(data[0])
		data = data[11:] // Skip 1 byte AuthPluginDataLen and 10 bytes reserved

		if authPluginDataLen > 8 {
			lenToRead := min(authPluginDataLen-8, len(data))
			packet.AuthPluginData = append(packet.AuthPluginData, data[:lenToRead]...)
			data = data[lenToRead:]
		}
	} else {
		data = data[10:] // Skip reserved 10 bytes if CLIENT_PLUGIN_AUTH is not set
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("handshake packet too short for AuthPluginName")
	}

	idx = bytes.IndexByte(data, 0x00)
	if idx == -1 {
		return nil, fmt.Errorf("malformed handshake packet: missing null terminator for AuthPluginName")
	}
	packet.AuthPluginName = string(data[:idx])

	return packet, nil
}

// Helper function to calculate minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func encodeHandshakePacket(packet *models.MySQLHandshakeV10Packet) ([]byte, error) {
	buf := new(bytes.Buffer)

	// Protocol version
	buf.WriteByte(packet.ProtocolVersion)

	// Server version
	buf.WriteString(packet.ServerVersion)
	buf.WriteByte(0x00) // Null terminator

	// Connection ID
	binary.Write(buf, binary.LittleEndian, packet.ConnectionID)

	// Auth-plugin-data-part-1 (first 8 bytes)
	if len(packet.AuthPluginData) < 8 {
		return nil, errors.New("auth plugin data too short")
	}
	buf.Write(packet.AuthPluginData[:8])

	// Filler
	buf.WriteByte(0x00)

	// Capability flags
	binary.Write(buf, binary.LittleEndian, uint16(packet.CapabilityFlags))
	// binary.Write(buf, binary.LittleEndian, uint16(packet.CapabilityFlags))

	// Character set
	buf.WriteByte(packet.CharacterSet)

	// Status flags
	binary.Write(buf, binary.LittleEndian, packet.StatusFlags)
	binary.Write(buf, binary.LittleEndian, uint16(packet.CapabilityFlags>>16))

	// Length of auth-plugin-data
	if packet.CapabilityFlags&0x800000 != 0 && len(packet.AuthPluginData) >= 21 {
		buf.WriteByte(byte(len(packet.AuthPluginData))) // Length of entire auth plugin data
	} else {
		buf.WriteByte(0x00)
	}
	// Reserved (10 zero bytes)
	buf.Write(make([]byte, 10))

	// Auth-plugin-data-part-2 (remaining auth data)
	if packet.CapabilityFlags&0x800000 != 0 && len(packet.AuthPluginData) >= 21 {
		buf.Write(packet.AuthPluginData[8:]) // Write all remaining bytes of auth plugin data
	}
	// Auth-plugin name
	if packet.CapabilityFlags&0x800000 != 0 {
		buf.WriteString(packet.AuthPluginName)
		buf.WriteByte(0x00) // Null terminator
	}

	return buf.Bytes(), nil
}
