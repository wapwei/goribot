package goribot

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/url"
	"sync"
	"time"
)

const (
	UserAgent      = "GoRibot"
	ThreadPoolSize = uint(15)
)

type PostDataType int

const (
	_                  PostDataType = iota
	TextPostData                    // text/plain
	UrlencodedPostData              // application/x-www-form-urlencoded
	JsonPostData                    // application/json
)

type ResponseHandler func(r *Response)

type Spider struct {
	UserAgent      string
	ThreadPoolSize uint
	DepthFirst     bool
	RandSleepRange [2]time.Duration
	Downloader     func(*Request) (*Response, error)

	pipeline     []PipelineInterface
	taskQueue    *TaskQueue
	taskChan     chan *Request
	taskFinished bool
	wg           sync.WaitGroup

	workingThread int32
}

func NewSpider() *Spider {
	return &Spider{
		taskQueue:      NewTaskQueue(),
		Downloader:     DoRequest,
		UserAgent:      UserAgent,
		DepthFirst:     false,
		ThreadPoolSize: ThreadPoolSize,
	}
}

func (s *Spider) Run() {
	if s.ThreadPoolSize == 0 {
		s.ThreadPoolSize = ThreadPoolSize
	}
	s.taskFinished = false
	s.taskChan = make(chan *Request)
	for i := uint(0); i < s.ThreadPoolSize; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for !s.taskFinished {
				select {
				case req := <-s.taskChan:
					func() {
						defer func() { s.workingThread -= 1 }()
						resp, err := s.Downloader(req)
						if err != nil {
							log.Println("Downloader Error", err, req.Url.String())
							s.handleOnErrorPipeline(err)
							return
						}
						resp = s.handleOnResponsePipeline(resp)
						if resp == nil {
							return
						}
						s.handleResponse(resp)
					}()
					break
				default:
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}
	for {
		if s.taskQueue.IsEmpty() {
			if s.workingThread == 0 { // make sure the queue is empty and no threat is working
				break
			} else {
				time.Sleep(1 * time.Millisecond)
			}
		} else {
			s.taskChan <- s.taskQueue.Pop()
			s.workingThread += 1
			randSleep(s.RandSleepRange[0], s.RandSleepRange[1])
		}
	}
	s.taskFinished = true
	s.wg.Wait()
}
func (s *Spider) handleResponse(response *Response) {
	for _, h := range response.Request.Handler {
		h(response)
	}
}

// Add a new task to the queue
func (s *Spider) Crawl(r *Request) {
	r.Header.Set("User-Agent", s.UserAgent)
	r = s.handleOnRequestPipeline(r)
	if r == nil {
		return
	}

	if s.DepthFirst {
		s.taskQueue.PushInHead(r)
	} else {
		s.taskQueue.Push(r)
	}
}
func (s *Spider) Get(u string, handler ...ResponseHandler) error {
	req, err := NewGetRequest(u)
	if err != nil {
		return err
	}
	req.Handler = handler
	s.Crawl(req)
	return nil
}
func (s *Spider) Post(u string, datatype PostDataType, rawdata interface{}, handler ...ResponseHandler) error {
	var data []byte
	ct := ""
	switch datatype {
	case TextPostData:
		ct = "text/plain"
		data = []byte(rawdata.(string))
		break
	case UrlencodedPostData:
		ct = "application/x-www-form-urlencoded"
		var urlS url.URL
		q := urlS.Query()
		for k, v := range rawdata.(map[string]string) {
			q.Add(k, v)
		}
		data = []byte(q.Encode())
		break
	case JsonPostData:
		ct = "application/json"
		tdata, err := json.Marshal(rawdata)
		if err != nil {
			return err
		}
		data = tdata
		break
	}
	req, err := NewPostRequest(u, data, ct)
	if err != nil {
		return err
	}
	req.Handler = handler
	s.Crawl(req)
	return nil
}

// Pipeline handlers and register
func (s *Spider) Use(p PipelineInterface) {
	p.Init(s)
	s.pipeline = append(s.pipeline, p)
}
func (s *Spider) handleInitPipeline() {
	for _, p := range s.pipeline {
		p.Init(s)
	}
}
func (s *Spider) handleOnRequestPipeline(r *Request) *Request {
	for _, p := range s.pipeline {
		r = p.OnRequest(s, r)
		if r == nil {
			return nil
		}
	}
	return r
}
func (s *Spider) handleOnResponsePipeline(r *Response) *Response {
	for _, p := range s.pipeline {
		r = p.OnResponse(s, r)
		if r == nil {
			return nil
		}
	}
	return r
}
func (s *Spider) handleOnErrorPipeline(err error) {
	for _, p := range s.pipeline {
		p.OnError(s, err)
	}
}
func (s *Spider) NewItem(item interface{}) {
	for _, p := range s.pipeline {
		item = p.OnItem(s, item)
		if item == nil {
			return
		}
	}
}

func randSleep(min, max time.Duration) {
	if min >= max || max == 0 {
		return
	}
	time.Sleep(time.Duration(rand.Int63n(int64(max)-int64(min)) + int64(min)))
}