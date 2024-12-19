RAW_FILE   ?= ./data/baguettes_raw.txt
CLEAN_FILE ?= ./data/baguettes_clean.tsv

VK_API_TOKEN ?= YOUR_TOKEN_GOES_HERE

.PHONY: scrap
scrap:
	go run ./scrapper -o=$(RAW_FILE) -t=$(VK_API_TOKEN)

.PHONY: clean
clean:
	go run ./cleaner -i=$(RAW_FILE) -o=$(CLEAN_FILE) -c=""
