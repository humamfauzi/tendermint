package keys

import (
	"io/ioutil"
	"math/big"
	"os"
	"strings"

	"github.com/pkg/errors"
)

const BankSize = 2048

// TODO: add error-checking codecs for invalid phrases

type Codec interface {
	BytesToWords([]byte) ([]string, error)
	WordsToBytes([]string) ([]byte, error)
}

type WordCodec struct {
	words []string
	bytes map[string]int
	check ECC
}

var _ Codec = WordCodec{}

func NewCodec(words []string) (codec WordCodec, err error) {
	if len(words) != BankSize {
		return codec, errors.Errorf("Bank must have %d words, found %d", BankSize, len(words))
	}

	res := WordCodec{
		words: words,
		// TODO: configure this outside???
		check: NewIEEECRC32(),
	}

	return res, nil
}

func LoadCodec(bank string) (codec WordCodec, err error) {
	words, err := loadBank(bank)
	if err != nil {
		return codec, err
	}
	return NewCodec(words)
}

// loadBank opens a wordlist file and returns all words inside
func loadBank(bank string) ([]string, error) {
	filename := "wordlist/" + bank + ".txt"
	words, err := getData(filename)
	if err != nil {
		return nil, err
	}
	wordsAll := strings.Split(strings.TrimSpace(words), "\n")
	return wordsAll, nil
}

// TODO: read from go-bind assets
func getData(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return "", errors.WithStack(err)
	}

	return string(data), nil
}

// given this many bytes, we will produce this many words
func wordlenFromBytes(numBytes int) int {
	// 2048 words per bank, which is 2^11.
	// 8 bits per byte, and we add +10 so it rounds up
	return (8*numBytes + 10) / 11
}

// given this many words, we will produce this many bytes.
// sometimes there are two possibilities.
// if maybeShorter is true, then represents len OR len-1 bytes
func bytelenFromWords(numWords int) (length int, maybeShorter bool) {
	// calculate the max number of complete bytes we could store in this word
	length = 11 * numWords / 8
	// if one less byte would also generate this length, set maybeShorter
	if wordlenFromBytes(length-1) == numWords {
		maybeShorter = true
	}
	return
}

// TODO: add checksum
func (c WordCodec) BytesToWords(raw []byte) (words []string, err error) {
	// always add a checksum to the data
	data := c.check.AddECC(raw)
	numWords := wordlenFromBytes(len(data))

	n2048 := big.NewInt(2048)
	nData := big.NewInt(0).SetBytes(data)
	nRem := big.NewInt(0)
	// Alternative, use condition "nData.BitLen() > 0"
	// to allow for shorter words when data has leading 0's
	for i := 0; i < numWords; i++ {
		nData.DivMod(nData, n2048, nRem)
		rem := nRem.Int64()
		w := c.words[rem]
		// double-check bank on generation...
		_, err := c.GetIndex(w)
		if err != nil {
			return nil, err
		}
		words = append(words, w)
	}
	return words, nil
}

func (c WordCodec) WordsToBytes(words []string) ([]byte, error) {
	l := len(words)
	n2048 := big.NewInt(2048)
	nData := big.NewInt(0)
	// since we output words based on the remainder, the first word has the lowest
	// value... we must load them in reverse order
	for i := 1; i <= l; i++ {
		rem, err := c.GetIndex(words[l-i])
		if err != nil {
			return nil, err
		}
		nRem := big.NewInt(int64(rem))
		nData.Mul(nData, n2048)
		nData.Add(nData, nRem)
	}

	// we copy into a slice of the expected size, so it is not shorter if there
	// are lots of leading 0s
	dataBytes := nData.Bytes()

	// copy into the container we have with the expected size
	outLen, flex := bytelenFromWords(len(words))
	toCheck := make([]byte, outLen)
	copy(toCheck[outLen-len(dataBytes):], dataBytes)

	// validate the checksum...
	output, err := c.check.CheckECC(toCheck)
	if flex && err != nil {
		// if flex, try again one shorter....
		toCheck = toCheck[1:]
		output, err = c.check.CheckECC(toCheck)
	}

	return output, err
}

// GetIndex finds the index of the words to create bytes
// Generates a map the first time it is loaded, to avoid needless
// computation when list is not used.
func (c WordCodec) GetIndex(word string) (int, error) {
	// generate the first time
	if c.bytes == nil {
		b := map[string]int{}
		for i, w := range c.words {
			if _, ok := b[w]; ok {
				return -1, errors.Errorf("Duplicate word in list: %s", w)
			}
			b[w] = i
		}
		c.bytes = b
	}

	// get the index, or an error
	rem, ok := c.bytes[word]
	if !ok {
		return -1, errors.Errorf("Unrecognized word: %s", word)
	}
	return rem, nil
}
