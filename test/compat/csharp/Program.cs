using System.Buffers.Binary;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using Google.Protobuf;
using Relay.V1;

if (args.Length != 1)
{
    throw new ArgumentException("expected exactly one fixture path");
}

using JsonDocument document = JsonDocument.Parse(File.ReadAllBytes(args[0]));
JsonElement fixture = document.RootElement;

uint revision = fixture.GetProperty("revision").GetUInt32();
ulong sequence = fixture.GetProperty("sequence").GetUInt64();
long expiryUnixMs = fixture.GetProperty("expiry_unix_ms").GetInt64();
string roomId = Text(fixture, "room_id");
string sessionId = Text(fixture, "session_id");
byte[] grantId = Hex(fixture, "grant_id_hex");
byte[] grantSecret = Hex(fixture, "grant_secret_hex");
byte[] candidateId = Hex(fixture, "candidate_id_hex");
byte[] clientNonce = Hex(fixture, "client_nonce_hex");
byte[] serverNonce = Hex(fixture, "server_nonce_hex");
byte[] bindingId = Hex(fixture, "binding_id_hex");
byte[] payload = Hex(fixture, "payload_hex");
byte[] expectedClientTag = Hex(fixture, "client_data_tag_hex");

byte[] clientWire = Hex(fixture, "client_data_envelope_hex");
Envelope parsedClient = Envelope.Parser.ParseFrom(clientWire);
Require(parsedClient.ProtocolRevision == revision, "ClientData revision");
Require(parsedClient.Sequence == sequence, "ClientData sequence");
RequireFixed(parsedClient.AuthTag.ToByteArray(), expectedClientTag, "ClientData envelope tag");
Require(parsedClient.SessionId == sessionId, "ClientData session ID");
Require(parsedClient.RoomId == roomId, "ClientData room ID");
Require(parsedClient.BodyCase == Envelope.BodyOneofCase.ClientData, "ClientData body");
RequireBytes(parsedClient.ClientData.BindingId.ToByteArray(), bindingId, "ClientData binding ID");
RequireBytes(parsedClient.ClientData.Payload.ToByteArray(), payload, "ClientData payload");

Envelope constructedClient = new()
{
    ProtocolRevision = revision,
    Sequence = sequence,
    AuthTag = ByteString.CopyFrom(expectedClientTag),
    SessionId = sessionId,
    RoomId = roomId,
    ClientData = new ClientData
    {
        BindingId = ByteString.CopyFrom(bindingId),
        Payload = ByteString.CopyFrom(payload),
    },
};
RequireBytes(constructedClient.ToByteArray(), clientWire, "constructed ClientData envelope");

byte[] serverWire = Hex(fixture, "server_data_envelope_hex");
Envelope parsedServer = Envelope.Parser.ParseFrom(serverWire);
Require(parsedServer.ProtocolRevision == revision, "ServerData revision");
Require(parsedServer.Sequence == sequence, "ServerData sequence");
Require(parsedServer.AuthTag.IsEmpty, "ServerData empty auth tag");
Require(parsedServer.SessionId == sessionId, "ServerData session ID");
Require(parsedServer.RoomId == roomId, "ServerData room ID");
Require(parsedServer.BodyCase == Envelope.BodyOneofCase.ServerData, "ServerData body");
Require(parsedServer.ServerData.SenderParticipantId == "player-a", "ServerData sender participant ID");
RequireBytes(parsedServer.ServerData.Payload.ToByteArray(), payload, "ServerData payload");

Envelope constructedServer = new()
{
    ProtocolRevision = revision,
    Sequence = sequence,
    SessionId = sessionId,
    RoomId = roomId,
    ServerData = new ServerData
    {
        SenderParticipantId = "player-a",
        Payload = ByteString.CopyFrom(payload),
    },
};
RequireBytes(constructedServer.ToByteArray(), serverWire, "constructed ServerData envelope");

byte[] revisionBytes = UInt32Bytes(revision);
byte[] sequenceBytes = UInt64Bytes(sequence);
byte[] expiryBytes = Int64Bytes(expiryUnixMs);

byte[] authFrame = Frame(
    "relay-auth-v1", revisionBytes, Ascii(roomId), Ascii(sessionId), grantId,
    candidateId, clientNonce, serverNonce);
RequireBytes(authFrame, Hex(fixture, "auth_frame_hex"), "AUTH frame");
RequireFixed(HMACSHA256.HashData(grantSecret, authFrame), Hex(fixture, "auth_tag_hex"), "AUTH tag");

byte[] bindingFrame = Frame(
    "relay-binding-key-v1", revisionBytes, Ascii(roomId), Ascii(sessionId), grantId,
    candidateId, clientNonce, serverNonce);
RequireBytes(bindingFrame, Hex(fixture, "binding_frame_hex"), "binding frame");
byte[] bindingKey = HMACSHA256.HashData(grantSecret, bindingFrame);
RequireFixed(bindingKey, Hex(fixture, "binding_key_hex"), "binding key");

byte[] boundFrame = Frame(
    "relay-bound-v1", revisionBytes, Ascii(roomId), Ascii(sessionId), candidateId,
    bindingId, expiryBytes);
RequireBytes(boundFrame, Hex(fixture, "bound_frame_hex"), "BOUND frame");
RequireFixed(HMACSHA256.HashData(bindingKey, boundFrame), Hex(fixture, "bound_tag_hex"), "BOUND tag");

byte[] clientDataFrame = Frame(
    "relay-client-data-v1", revisionBytes, Ascii(roomId), Ascii(sessionId), bindingId,
    sequenceBytes, payload);
RequireBytes(clientDataFrame, Hex(fixture, "client_data_frame_hex"), "ClientData frame");
RequireFixed(HMACSHA256.HashData(bindingKey, clientDataFrame), expectedClientTag, "ClientData tag");

byte[] pingFrame = Frame(
    "relay-ping-v1", revisionBytes, Ascii(roomId), Ascii(sessionId), bindingId, sequenceBytes);
RequireBytes(pingFrame, Hex(fixture, "ping_frame_hex"), "Ping frame");
RequireFixed(HMACSHA256.HashData(bindingKey, pingFrame), Hex(fixture, "ping_tag_hex"), "Ping tag");

Console.WriteLine("protocol compatibility OK");

static string Text(JsonElement fixture, string name) =>
    fixture.GetProperty(name).GetString() ?? throw new InvalidDataException($"{name} is null");

static byte[] Hex(JsonElement fixture, string name) => Convert.FromHexString(Text(fixture, name));

static byte[] Ascii(string value) => Encoding.ASCII.GetBytes(value);

static byte[] UInt32Bytes(uint value)
{
    byte[] bytes = new byte[sizeof(uint)];
    BinaryPrimitives.WriteUInt32BigEndian(bytes, value);
    return bytes;
}

static byte[] UInt64Bytes(ulong value)
{
    byte[] bytes = new byte[sizeof(ulong)];
    BinaryPrimitives.WriteUInt64BigEndian(bytes, value);
    return bytes;
}

static byte[] Int64Bytes(long value)
{
    byte[] bytes = new byte[sizeof(long)];
    BinaryPrimitives.WriteInt64BigEndian(bytes, value);
    return bytes;
}

static byte[] Frame(string domain, params byte[][] fields)
{
    byte[] domainBytes = Ascii(domain);
    int length = sizeof(ushort) + domainBytes.Length;
    foreach (byte[] field in fields)
    {
        length = checked(length + sizeof(uint) + field.Length);
    }

    byte[] frame = new byte[length];
    int offset = 0;
    BinaryPrimitives.WriteUInt16BigEndian(frame.AsSpan(offset, sizeof(ushort)), checked((ushort)domainBytes.Length));
    offset += sizeof(ushort);
    domainBytes.CopyTo(frame, offset);
    offset += domainBytes.Length;

    foreach (byte[] field in fields)
    {
        BinaryPrimitives.WriteUInt32BigEndian(frame.AsSpan(offset, sizeof(uint)), checked((uint)field.Length));
        offset += sizeof(uint);
        field.CopyTo(frame, offset);
        offset += field.Length;
    }

    return frame;
}

static void Require(bool condition, string name)
{
    if (!condition)
    {
        throw new InvalidDataException($"{name} mismatch");
    }
}

static void RequireBytes(byte[] actual, byte[] expected, string name) =>
    Require(actual.AsSpan().SequenceEqual(expected), name);

static void RequireFixed(byte[] actual, byte[] expected, string name) =>
    Require(CryptographicOperations.FixedTimeEquals(actual, expected), name);
