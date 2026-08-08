package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

type Bytes16 [16]byte
type Bytes32 [32]byte

func AuthTag(
	grantSecret Bytes32,
	revision uint32,
	roomID, sessionID string,
	grantID, candidateID, clientNonce Bytes16,
	serverNonce Bytes32,
) Bytes32 {
	return handshakeTag(
		grantSecret, "relay-auth-v1", revision, roomID, sessionID,
		grantID, candidateID, clientNonce, serverNonce,
	)
}

func BindingKey(
	grantSecret Bytes32,
	revision uint32,
	roomID, sessionID string,
	grantID, candidateID, clientNonce Bytes16,
	serverNonce Bytes32,
) Bytes32 {
	return handshakeTag(
		grantSecret, "relay-binding-key-v1", revision, roomID, sessionID,
		grantID, candidateID, clientNonce, serverNonce,
	)
}

func BoundTag(
	bindingKey Bytes32,
	revision uint32,
	roomID, sessionID string,
	candidateID, bindingID Bytes16,
	expiryUnixMS int64,
) Bytes32 {
	var revisionBytes [4]byte
	binary.BigEndian.PutUint32(revisionBytes[:], revision)
	var expiryBytes [8]byte
	binary.BigEndian.PutUint64(expiryBytes[:], uint64(expiryUnixMS))

	return transcriptHMAC(
		bindingKey, "relay-bound-v1", revisionBytes[:], []byte(roomID), []byte(sessionID),
		candidateID[:], bindingID[:], expiryBytes[:],
	)
}

func ClientDataTag(
	bindingKey Bytes32,
	revision uint32,
	roomID, sessionID string,
	bindingID Bytes16,
	sequence uint64,
	payload []byte,
) Bytes32 {
	var revisionBytes [4]byte
	binary.BigEndian.PutUint32(revisionBytes[:], revision)
	var sequenceBytes [8]byte
	binary.BigEndian.PutUint64(sequenceBytes[:], sequence)

	return transcriptHMAC(
		bindingKey, "relay-client-data-v1", revisionBytes[:], []byte(roomID), []byte(sessionID),
		bindingID[:], sequenceBytes[:], payload,
	)
}

func PingTag(
	bindingKey Bytes32,
	revision uint32,
	roomID, sessionID string,
	bindingID Bytes16,
	sequence uint64,
) Bytes32 {
	var revisionBytes [4]byte
	binary.BigEndian.PutUint32(revisionBytes[:], revision)
	var sequenceBytes [8]byte
	binary.BigEndian.PutUint64(sequenceBytes[:], sequence)

	return transcriptHMAC(
		bindingKey, "relay-ping-v1", revisionBytes[:], []byte(roomID), []byte(sessionID),
		bindingID[:], sequenceBytes[:],
	)
}

func EqualTag(expected Bytes32, actual []byte) bool {
	return len(actual) == len(expected) && hmac.Equal(expected[:], actual)
}

func handshakeTag(
	grantSecret Bytes32,
	domain string,
	revision uint32,
	roomID, sessionID string,
	grantID, candidateID, clientNonce Bytes16,
	serverNonce Bytes32,
) Bytes32 {
	var revisionBytes [4]byte
	binary.BigEndian.PutUint32(revisionBytes[:], revision)

	return transcriptHMAC(
		grantSecret, domain, revisionBytes[:], []byte(roomID), []byte(sessionID), grantID[:],
		candidateID[:], clientNonce[:], serverNonce[:],
	)
}

func transcriptHMAC(key Bytes32, domain string, fields ...[]byte) Bytes32 {
	mac := hmac.New(sha256.New, key[:])
	var length [4]byte
	binary.BigEndian.PutUint16(length[:2], uint16(len(domain)))
	_, _ = mac.Write(length[:2])
	_, _ = mac.Write([]byte(domain))

	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = mac.Write(length[:])
		_, _ = mac.Write(field)
	}

	var output Bytes32
	mac.Sum(output[:0])
	return output
}
