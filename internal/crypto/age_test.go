package crypto

import (
	"bytes"
	"io"
	"testing"
)

func TestAgeEncryptionRoundTrip(t *testing.T) {
	// 1. Generate keypair
	kp1, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair 1 failed: %v", err)
	}

	kp2, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair 2 failed: %v", err)
	}

	testPayload := []byte("The beaver builds strong lodges for the winter cache.")

	// 2. Encrypt for both recipients
	var cipherBuffer bytes.Buffer
	encWriter, err := EncryptStream(&cipherBuffer, []string{kp1.PublicKey, kp2.PublicKey})
	if err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	if _, err := encWriter.Write(testPayload); err != nil {
		t.Fatalf("Write to encWriter failed: %v", err)
	}
	if err := encWriter.Close(); err != nil {
		t.Fatalf("Close encWriter failed: %v", err)
	}

	// 3. Test decryption with recipient 1
	decReader1, err := DecryptStream(bytes.NewReader(cipherBuffer.Bytes()), kp1.SecretKey)
	if err != nil {
		t.Fatalf("DecryptStream with key 1 failed: %v", err)
	}
	recovered1, err := io.ReadAll(decReader1)
	if err != nil {
		t.Fatalf("ReadAll decrypted 1 failed: %v", err)
	}
	decReader1.Close()

	if string(recovered1) != string(testPayload) {
		t.Errorf("Recovered text mismatch: got '%s', want '%s'", string(recovered1), string(testPayload))
	}

	// 4. Test decryption with recipient 2 (multi-recipient validation)
	decReader2, err := DecryptStream(bytes.NewReader(cipherBuffer.Bytes()), kp2.SecretKey)
	if err != nil {
		t.Fatalf("DecryptStream with key 2 failed: %v", err)
	}
	recovered2, err := io.ReadAll(decReader2)
	if err != nil {
		t.Fatalf("ReadAll decrypted 2 failed: %v", err)
	}
	decReader2.Close()

	if string(recovered2) != string(testPayload) {
		t.Errorf("Recovered text with key 2 mismatch: got '%s', want '%s'", string(recovered2), string(testPayload))
	}
}

func TestParseRecipientOrIdentity(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair failed: %v", err)
	}

	// 1. Passing public key directly
	pub1, err := ParseRecipientOrIdentity(kp.PublicKey)
	if err != nil {
		t.Fatalf("ParseRecipientOrIdentity on public key failed: %v", err)
	}
	if pub1 != kp.PublicKey {
		t.Errorf("expected %s, got %s", kp.PublicKey, pub1)
	}

	// 2. Passing secret key derives corresponding public key
	pub2, err := ParseRecipientOrIdentity(kp.SecretKey)
	if err != nil {
		t.Fatalf("ParseRecipientOrIdentity on secret key failed: %v", err)
	}
	if pub2 != kp.PublicKey {
		t.Errorf("expected derived public key %s, got %s", kp.PublicKey, pub2)
	}

	// 3. User's exact key from chat:
	userSecret := "AGE-SECRET-KEY-1X8SF2CARZNKV078FXM0LMDJ86FR9KP3ES5SLZ9L9U4S7LCTSDR6S4AYU80"
	expectedUserPub := "age1lu4m2262yxwt5wehqc7f6pj7msncluh4j8pe6lw49206v3amuefsxcj40s"
	derivedUserPub, err := ParseRecipientOrIdentity(userSecret)
	if err != nil {
		t.Fatalf("ParseRecipientOrIdentity on user secret failed: %v", err)
	}
	if derivedUserPub != expectedUserPub {
		t.Errorf("expected derived public key %s, got %s", expectedUserPub, derivedUserPub)
	}

	// 4. Invalid input fails
	if _, err := ParseRecipientOrIdentity("invalid-key-string"); err == nil {
		t.Errorf("expected error for invalid key string, got nil")
	}
}
