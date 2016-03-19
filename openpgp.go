package archive

import (
	"fmt"

	"crypto"
	"crypto/sha512"

	"golang.org/x/crypto/openpgp/packet"
	"pault.ag/go/blobstore"
)

func (a Archive) encodeSigned(
	data interface{},
) (*blobstore.Object, *blobstore.Object, error) {
	if a.signingKey == nil {
		return nil, nil, fmt.Errorf("No signing key loaded")
	}

	signature, err := a.store.Create()
	if err != nil {
		return nil, nil, err
	}
	defer signature.Close()

	hash := sha512.New()

	obj, err := a.encode(data, hash)
	if err != nil {
		return nil, nil, err
	}

	sig := new(packet.Signature)
	sig.SigType = packet.SigTypeBinary
	sig.PubKeyAlgo = a.signingKey.PrivateKey.PubKeyAlgo

	sig.Hash = crypto.SHA512

	sig.CreationTime = new(packet.Config).Now()
	sig.IssuerKeyId = &(a.signingKey.PrivateKey.KeyId)

	err = sig.Sign(hash, a.signingKey.PrivateKey, nil)
	if err != nil {
		return nil, nil, err
	}

	if err := sig.Serialize(signature); err != nil {
		return nil, nil, err
	}

	sigObj, err := a.store.Commit(*signature)
	if err != nil {
		return nil, nil, err
	}

	return obj, sigObj, nil
}
