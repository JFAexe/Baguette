package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	badPosts  = regexp.MustCompile(`(://|#БТnews|\[CLUB\d+\|.+\])`)
	groupTag  = regexp.MustCompile(`@(BUGURT_THREAD|bugurt_thread)`)
	strayText = regexp.MustCompile(`(\n\n|\n\A.+$)`)
	hashtags  = regexp.MustCompile(`^(#\S+)`)
)

var (
	InputFilePath  string
	OutputFilePath string
	PostSeparator  string
	Context        string
	TokenBOS       string
	TokenEOS       string
	TokenPAD       string
	Limit          int
	TSV            bool
)

func main() {
	flag.StringVar(&InputFilePath, "i", "raw.txt", "input file path")
	flag.StringVar(&OutputFilePath, "o", "clean.tsv", "output file path")
	flag.StringVar(&PostSeparator, "r", "<BAGUETTE>", "raw posts separator")
	flag.StringVar(&Context, "c", "НАПИШИ БАГЕТ", "context")
	flag.StringVar(&TokenBOS, "b", "<BOS>", "bos token")
	flag.StringVar(&TokenEOS, "e", "<EOS>", "eos token")
	flag.StringVar(&TokenPAD, "p", "<PAD>", "pad token")
	flag.IntVar(&Limit, "l", 0, "processed posts limit")
	flag.BoolVar(&TSV, "t", true, "save as tsv")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	inputAbs, err := filepath.Abs(InputFilePath)
	if err != nil {
		return fmt.Errorf("failed to transform input file path to absolute: %w", err)
	}

	input, err := os.Open(inputAbs)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}
	defer input.Close()

	log.Printf("parsing posts from %q", inputAbs)

	outputAbs, err := filepath.Abs(OutputFilePath)
	if err != nil {
		return fmt.Errorf("failed to transform output file path to absolute: %w", err)
	}

	output, err := os.Create(outputAbs)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer output.Close()

	log.Printf("saving posts to %q", outputAbs)

	var (
		readBuffer, writeBuffer bytes.Buffer

		scanner = bufio.NewScanner(input)
	)

	log.Printf("processing posts, limit %d", Limit)

	if TSV {
		output.WriteString("id\tbaguette\n")
	}

	var count int

	for scanner.Scan() {
		if scanner.Text() != PostSeparator {
			readBuffer.Write(scanner.Bytes())
			readBuffer.WriteString("\n")

			continue
		}

		ok, err := processPost(&readBuffer, &writeBuffer)
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		count++

		if TSV {
			output.WriteString(strconv.Itoa(count))
			output.WriteString("\t")
		}

		if strings.TrimSpace(Context) != "" {
			output.WriteString(Context)
			output.WriteString(" ")
		}

		output.WriteString(TokenBOS)
		output.WriteString(" ")
		output.Write(writeBuffer.Bytes())
		output.WriteString(" ")
		output.WriteString(TokenEOS)
		output.WriteString("\n")

		if Limit > 0 && count >= Limit || ctx.Err() != nil {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to iterate over input file: %w", err)
	}

	log.Printf("processed %d posts", count)

	return nil
}

func processPost(r *bytes.Buffer, w *bytes.Buffer) (bool, error) {
	w.Reset()
	defer r.Reset()

	noTag := groupTag.ReplaceAll(r.Bytes(), []byte(""))

	if !bytes.ContainsAny(noTag, "@>") || badPosts.Match(noTag) {
		return false, nil
	}

	var (
		t bytes.Buffer

		scanner = bufio.NewScanner(bytes.NewReader(noTag))
	)

	for scanner.Scan() {
		processed := bytes.TrimSpace(scanner.Bytes())
		processed = bytes.ToTitle(processed)

		split := bytes.Split(processed, []byte(" @ "))
		processed = bytes.Join(split, []byte("\n@\n"))

		t.Write(processed)
		t.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("failed to iterate over post: %w", err)
	}

	scanner = bufio.NewScanner(bytes.NewReader(t.Bytes()))

	t.Reset()

	for scanner.Scan() {
		if len(hashtags.Find(scanner.Bytes())) == 0 {
			t.Write(scanner.Bytes())
			t.WriteString("\n")

			continue
		}

		split := bytes.SplitN(scanner.Bytes(), []byte(" "), 2)

		for {
			if len(split) < 2 {
				t.Write(split[0])
				t.WriteString("\n")

				break
			}

			t.Write(split[0])
			t.WriteString("\n@\n")

			old := split[1]

			if split = bytes.SplitN(split[1], []byte(" "), 2); !bytes.HasPrefix(split[0], []byte("#")) {
				t.Write(old)
				t.WriteString("\n")

				break
			}
		}
	}

	result := bytes.TrimSpace(t.Bytes())

	if bytes.Count(result, []byte("\n")) < 2 {
		return false, nil
	}

	result = strayText.ReplaceAll(result, []byte("\n@\n"))
	result = bytes.TrimSuffix(result, []byte("@"))
	result = bytes.ReplaceAll(result, []byte("\n"), []byte(" "))
	result = bytes.ReplaceAll(result, []byte("@ @ @"), []byte("@@@"))
	result = bytes.ReplaceAll(result, []byte("@"), []byte(TokenPAD))

	w.Write(bytes.ToValidUTF8(bytes.Join(bytes.Fields(result), []byte(" ")), []byte("")))

	return true, nil
}
