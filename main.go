package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/mb-14/gomarkov"
)

const chainFile = "markov.json"

var tokenBoundary = regexp.MustCompile(`\s+`)

func splitParagraph(s string) []string {
	tokens := tokenBoundary.Split(s, -1)
	return tokens
}

func main() {
	var (
		training bool
		order    int
		start    int
		end      int
		workers  int

		n int
	)

	flag.BoolVar(&training, "training", false, "do train the markov chin")
	flag.IntVar(&order, "order", 4, "markov chain order")
	flag.IntVar(&start, "start", 1, "start page number (inclusive)")
	flag.IntVar(&end, "end", 1, "end page number (non-inclusive)")
	flag.IntVar(&workers, "workers", runtime.NumCPU(), "parallel training")

	flag.IntVar(&n, "n", 1, "generate n times")
	flag.Parse()

	if training {
		train(order, start, end, workers)
		return
	}

	chain, err := loadChain(order)
	if err != nil {
		log.Fatalln("cnanot load markov chain:", err)
	}

	for i := 0; i < n; i++ {
		tokens, err := generate(chain)
		if err != nil {
			log.Fatalln("fail to generate tokens:", err)
		}
		fmt.Println(strings.Join(tokens, " "))
	}
}

func generate(chain *gomarkov.Chain) ([]string, error) {
	tokens := make([]string, chain.Order)
	for i := range tokens {
		tokens[i] = gomarkov.StartToken
	}
	for tokens[len(tokens)-1] != gomarkov.EndToken {
		next, err := chain.Generate(tokens[len(tokens)-chain.Order:])
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, next)
	}
	return tokens[chain.Order : len(tokens)-1], nil
}

func loadChain(order int) (*gomarkov.Chain, error) {
	chain := gomarkov.NewChain(order)
	r, err := os.Open(chainFile)
	if err != nil && os.IsNotExist(err) {
		return chain, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err = json.NewDecoder(r).Decode(chain); err != nil {
		return nil, err
	}
	return chain, nil
}

func saveChain(chain *gomarkov.Chain) error {
	w, err := os.Create(chainFile)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(w).Encode(chain); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func train(order int, start, end, workers int) {
	chain, err := loadChain(order)
	if err != nil {
		log.Fatalln("cannot load markov chain:", err)
	}

	var chainLock sync.Mutex
	done := make(chan struct{})

	piece := (end - start) / workers
	rem := (end - start) % workers
	for i := 0; i < workers; i++ {
		p := piece
		if rem > 0 {
			rem--
			p++
		}
		go func(start, end int) {
			for j := start; j < end; j++ {
				raws, err := readPage(j)
				if err != nil {
					log.Fatalf("cannot read page#%d: %v", i, err)
				}
				chainLock.Lock()
				for _, r := range raws {
					chain.Add(splitParagraph(r.title))
					log.Printf("Add #%d: %q", r.number, r.title)
				}
				chainLock.Unlock()
			}
			done <- struct{}{}
		}(start, start+p)
		start += p
	}

	for i := 0; i < workers; i++ {
		<-done
	}
	if err = saveChain(chain); err != nil {
		log.Fatal("cannot save markov chain:", err)
	}
}

type brief struct {
	number int64
	title  string
}

func readPage(i int) ([]*brief, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://coinpan.com/index.php?page=%d&mid=free", i), nil)
	req.Header.Add("Accept", "*/*")
	req.Header.Add("Accept-Language", "en")
	req.Header.Add("Referer", "https://coinpan.com/free")
	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36 OPR/56.0.3051.99")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	document, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		return nil, err
	}
	trs := document.Find(".board_list tbody tr:not(.notice)")
	raws := make([]*brief, trs.Length())
	trs.Each(func(i int, tr *goquery.Selection) {
		numberStr := strings.TrimSpace(tr.Find("td.no span.number").Text())
		number, _ := strconv.ParseInt(numberStr, 10, 64)
		title := strings.TrimSpace(tr.Find("td.title a").First().Text())
		raws[i] = &brief{
			number: number,
			title:  title,
		}
	})
	return raws, nil
}
