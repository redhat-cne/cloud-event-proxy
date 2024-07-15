package subscriber

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/types"

	"github.com/pkg/errors"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/store"

	log "github.com/sirupsen/logrus"

	SubscriberStore "github.com/redhat-cne/sdk-go/pkg/store/subscriber"
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
)

// API ... api methods  for publisher subscriber
type API struct {
	SubscriberStore  *SubscriberStore.Store //  each client will have one store
	storeFilePath    string                 // subscribers
	transportEnabled bool                   //  http  is enabled
}

var instance *API
var once sync.Once
var mu sync.Mutex

// NewSubscriber create new subscribers connections
func NewSubscriber(clientID uuid.UUID) subscriber.Subscriber {
	return subscriber.Subscriber{
		ClientID: clientID,
		SubStore: &store.PubSubStore{
			RWMutex: sync.RWMutex{},
			Store:   nil,
		},
		EndPointURI: nil,
		Status:      1,
	}
}

// New creates empty publisher or subscriber
func New() subscriber.Subscriber {
	return subscriber.Subscriber{}
}

// GetAPIInstance get event instance
func GetAPIInstance(storeFilePath string) *API {
	once.Do(func() {
		instance = &API{
			transportEnabled: true,
			SubscriberStore: &SubscriberStore.Store{
				RWMutex: sync.RWMutex{},
				Store:   map[uuid.UUID]*subscriber.Subscriber{},
			},
			storeFilePath: storeFilePath,
		}
		hasDir(storeFilePath)
		instance.ReloadStore()
	})
	return instance
}

// ReloadStore reload store if there is any change or refresh is required
func (p *API) ReloadStore() {
	// load for file
	log.Infof("reloading subscribers from the store %s", p.storeFilePath)
	if files, err := loadFileNamesFromDir(p.storeFilePath); err == nil {
		for _, f := range files {
			// valid subscription filename is <uuid>.json
			if uuid.Validate(strings.Split(f, ".")[0]) != nil {
				continue
			} else if b, err1 := loadFromFile(fmt.Sprintf("%s/%s", p.storeFilePath, f)); err1 == nil {
				if len(b) > 0 {
					var sub subscriber.Subscriber
					var err2 error
					if err2 = json.Unmarshal(b, &sub); err2 == nil {
						if sub.ClientID != uuid.Nil {
							p.SubscriberStore.Set(sub.ClientID, sub)
						} else {
							log.Errorf("subscriber data from file %s is not valid", f)
						}
					} else {
						log.Errorf("error parsing subscriber %s \n %s", string(b), err2.Error())
					}
				}
			}
		}
	}
	log.Infof("%d registered clients reloaded", len(p.SubscriberStore.Store))
	for k, v := range p.SubscriberStore.Store {
		log.Infof("registered clients %s : %s", k, v.String())
	}
}

// HasTransportEnabled ...
func (p *API) HasTransportEnabled() bool {
	return p.transportEnabled
}

// DisableTransport ...
func (p *API) DisableTransport() {
	p.transportEnabled = false
}

// EnableTransport ...
func (p *API) EnableTransport() {
	p.transportEnabled = true
}

// ClientCount .. client cound
func (p *API) ClientCount() int {
	return len(p.SubscriberStore.Store)
}

// GetSubFromSubscriptionsStore get data from publisher store
func (p *API) GetSubFromSubscriptionsStore(clientID uuid.UUID, address string) (pubsub.PubSub, error) {
	if subscriber, ok := p.HasClient(clientID); ok {
		for _, sub := range subscriber.SubStore.Store {
			if sub.GetResource() == address {
				return pubsub.PubSub{
					Version:     sub.Version,
					ID:          sub.ID,
					EndPointURI: sub.EndPointURI,
					URILocation: sub.URILocation,
					Resource:    sub.Resource,
				}, nil
			}
		}
	}

	return pubsub.PubSub{}, fmt.Errorf("publisher not found for address %s", address)
}

// HasSubscription check if the subscriptionOne is already exists in the store/cache
func (p *API) HasSubscription(clientID uuid.UUID, address string) (pubsub.PubSub, bool) {
	if sub, err := p.GetSubFromSubscriptionsStore(clientID, address); err == nil {
		return sub, true
	}
	return pubsub.PubSub{}, false
}

// HasClient check if  client is already exists in the store/cache
func (p *API) HasClient(clientID uuid.UUID) (*subscriber.Subscriber, bool) {
	if subscriber, ok := p.SubscriberStore.Get(clientID); ok {
		return &subscriber, true
	}
	return nil, false
}

// CreateSubscription create a subscriptionOne and store it in a file and cache
func (p *API) CreateSubscription(clientID uuid.UUID, sub subscriber.Subscriber) (subscriptionClient *subscriber.Subscriber, err error) {
	var ok bool
	if subscriptionClient, ok = p.HasClient(clientID); !ok {
		subscriptionClient = subscriber.New(clientID)
	}
	subscriptionClient.ResetFailCount()
	_ = subscriptionClient.SetEndPointURI(sub.GetEndPointURI())
	subscriptionClient.SetStatus(subscriber.Active)
	subscriptionClient.Action = channel.NEW
	pubStore := subscriptionClient.GetSubStore()
	var hasResource bool
	for key, value := range sub.SubStore.Store {
		hasResource = false
		for _, s := range pubStore.Store {
			if s.Resource == value.Resource {
				hasResource = true
				continue
			}
		}
		if !hasResource {
			if key == "" {
				key = uuid.New().String()
			}
			subscriptionClient.SubStore.Set(key, *value)
		}
	}
	p.SubscriberStore.Set(clientID, *subscriptionClient)
	// persist the subscriptionOne -
	err = writeToFile(*subscriptionClient, fmt.Sprintf("%s/%s", p.storeFilePath, fmt.Sprintf("%s.json", clientID)))
	if err != nil {
		log.Errorf("error writing to a store %v\n", err)
		return nil, err
	}
	log.Infof("subscription persisted into a file %s", fmt.Sprintf("%s/%s  - content %s", p.storeFilePath, fmt.Sprintf("%s.json", clientID), subscriptionClient.String()))
	// store the publisher

	return subscriptionClient, nil
}

// GetSubscriptionClient  get a clientID by id
func (p *API) GetSubscriptionClient(clientID uuid.UUID) (subscriber.Subscriber, error) {
	if subs, ok := p.SubscriberStore.Get(clientID); ok {
		return subs, nil
	}
	return subscriber.Subscriber{}, fmt.Errorf("subscriber data was not found for id %s", clientID)
}

// GetSubscriptionsFromFile  get subscriptions data from the file store
func (p *API) GetSubscriptionsFromFile(clientID uuid.UUID) ([]byte, error) {
	b, err := loadFromFile(fmt.Sprintf("%s/%s", p.storeFilePath, fmt.Sprintf("%s.json", clientID.String())))
	return b, err
}

// GetSubscriptionsFromClientID get all subs from the client
func (p *API) GetSubscriptionsFromClientID(clientID uuid.UUID) (sub map[string]*pubsub.PubSub) {
	if subs, ok := p.SubscriberStore.Get(clientID); ok {
		sub = subs.SubStore.Store
	}
	return
}

// GetSubscriptions get all subs
func (p *API) GetSubscriptions() ([]byte, error) {
	p.SubscriberStore.RLock()
	defer p.SubscriberStore.RUnlock()
	var allSubs []pubsub.PubSub
	for _, s := range p.SubscriberStore.Store {
		for _, sub := range s.SubStore.Store {
			allSubs = append(allSubs, *sub)
		}
	}
	return json.MarshalIndent(&allSubs, "", " ")
}

// GetSubscription get sub info from clientID and subID
func (p *API) GetSubscription(clientID uuid.UUID, subID string) (pubsub.PubSub, error) {
	if subs, ok := p.SubscriberStore.Get(clientID); ok {
		return subs.Get(subID), nil
	}
	return pubsub.PubSub{}, fmt.Errorf("subscription data was not found for id %s", subID)
}

// GetSubscriberURLByResourceAndClientID  get  subscription information by client id/resource
func (p *API) GetSubscriberURLByResourceAndClientID(clientID uuid.UUID, resource string) (url *string) {
	p.SubscriberStore.RLock()
	defer p.SubscriberStore.RUnlock()
	for _, subs := range p.SubscriberStore.Store {
		if subs.ClientID == clientID {
			for _, sub := range subs.SubStore.Store {
				if sub.GetResource() == resource {
					return func(s string) *string {
						return &s
					}(subs.GetEndPointURI())
				}
			}
		}
	}
	return nil
}

// GetSubscriberURLByResource  get  subscriptionOne information
func (p *API) GetSubscriberURLByResource(resource string) (urls []string) {
	p.SubscriberStore.RLock()
	defer p.SubscriberStore.RUnlock()
	for _, subs := range p.SubscriberStore.Store {
		for _, sub := range subs.SubStore.Store {
			if sub.GetResource() == resource {
				urls = append(urls, subs.GetEndPointURI())
			}
		}
	}
	return urls
}

// GetClientIDByResource  get  subscriptionOne information
func (p *API) GetClientIDByResource(resource string) (clientIDs []uuid.UUID) {
	p.SubscriberStore.RLock()
	defer p.SubscriberStore.RUnlock()
	for _, subs := range p.SubscriberStore.Store {
		for _, sub := range subs.SubStore.Store {
			if sub.GetResource() == resource {
				clientIDs = append(clientIDs, subs.ClientID)
			}
		}
	}
	return clientIDs
}

// GetClientIDBySubID ...
func (p *API) GetClientIDBySubID(subID string) (clientIDs []uuid.UUID) {
	p.SubscriberStore.RLock()
	defer p.SubscriberStore.RUnlock()
	for _, subs := range p.SubscriberStore.Store {
		for _, sub := range subs.SubStore.Store {
			if sub.GetID() == subID {
				clientIDs = append(clientIDs, subs.ClientID)
			}
		}
	}
	return clientIDs
}

// GetClientIDAddressByResource  get  subscriptionOne information
func (p *API) GetClientIDAddressByResource(resource string) map[uuid.UUID]*types.URI {
	clients := map[uuid.UUID]*types.URI{}
	p.SubscriberStore.RLock()
	defer p.SubscriberStore.RUnlock()
	for _, subs := range p.SubscriberStore.Store {
		for _, sub := range subs.SubStore.Store {
			if sub.GetResource() == resource {
				clients[subs.ClientID] = subs.EndPointURI
			}
		}
	}
	return clients
}

// DeleteSubscription delete a subscriptionOne by id
func (p *API) DeleteSubscription(clientID uuid.UUID, subscriptionID string) error {
	if subStore, ok := p.SubscriberStore.Get(clientID); ok { // client found
		if sub, ok2 := subStore.SubStore.Store[subscriptionID]; ok2 {
			err := deleteFromFile(*sub, fmt.Sprintf("%s/%s", p.storeFilePath, fmt.Sprintf("%s.json", clientID)))
			subStore.SubStore.Delete(subscriptionID)
			p.SubscriberStore.Set(clientID, subStore)
			return err
		}
	}
	return nil
}

// DeleteAllSubscriptions  delete all subscriptionOne information
func (p *API) DeleteAllSubscriptions(clientID uuid.UUID) error {
	if subStore, ok := p.SubscriberStore.Get(clientID); ok {
		if err := deleteAllFromFile(fmt.Sprintf("%s/%s", p.storeFilePath, fmt.Sprintf("%s.json", clientID))); err != nil {
			return err
		}
		subStore.SubStore = &store.PubSubStore{
			RWMutex: sync.RWMutex{},
			Store:   map[string]*pubsub.PubSub{},
		}
		p.SubscriberStore.Set(clientID, subStore)
	}
	return nil
}

// DeleteClient  delete all subscriptionOne information
func (p *API) DeleteClient(clientID uuid.UUID) error {
	if _, ok := p.SubscriberStore.Get(clientID); ok { // client found
		log.Infof("delete from file %s",
			fmt.Sprintf("%s/%s", p.storeFilePath, fmt.Sprintf("%s.json", clientID)))
		if err := deleteAllFromFile(fmt.Sprintf("%s/%s", p.storeFilePath, fmt.Sprintf("%s.json", clientID))); err != nil {
			return err
		}
		p.SubscriberStore.Delete(clientID)
	} else {
		log.Infof("subscription for client id %s not found", clientID)
	}
	return nil
}

// UpdateStatus .. update status
func (p *API) UpdateStatus(clientID uuid.UUID, status subscriber.Status) error {
	if subStore, ok := p.SubscriberStore.Get(clientID); ok {
		subStore.SetStatus(status)
		p.SubscriberStore.Set(clientID, subStore)
		// do not write to file , if restarts it will consider all client are active
	} else {
		return errors.New("failed to update subscriber status")
	}
	return nil
}

// IncFailCountToFail .. update fail count
func (p *API) IncFailCountToFail(clientID uuid.UUID) bool {
	if subStore, ok := p.SubscriberStore.Get(clientID); ok {
		subStore.IncFailCount()
		p.SubscriberStore.Set(clientID, subStore)
		if subStore.Action == channel.DELETE {
			return true
		}
	}
	return false
}

// FailCountThreshold .. get threshold
func (p *API) FailCountThreshold() int {
	return subscriber.SetConnectionToFailAfter
}

func (p *API) GetFailCount(clientID uuid.UUID) int {
	if subStore, ok := p.SubscriberStore.Get(clientID); ok {
		return subStore.FailedCount()
	}
	return 0
}

// SubscriberMarkedForDelete ...
func (p *API) SubscriberMarkedForDelete(clientID uuid.UUID) bool {
	if subStore, ok := p.SubscriberStore.Get(clientID); ok {
		if subStore.Action == channel.DELETE {
			return true
		}
	}
	return false
}

// deleteAllFromFile deletes  publisher and subscriptionOne information from the file system
func deleteAllFromFile(filePath string) error {
	return os.Remove(filePath)
}

// DeleteFromFile is used to delete subscriptionOne from the file system
func deleteFromFile(sub pubsub.PubSub, filePath string) error {
	var persistedSubClient subscriber.Subscriber
	//open file
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	//read file and unmarshall json file to slice of users
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	if len(b) > 0 {
		err = json.Unmarshal(b, &persistedSubClient)
		if err != nil {
			return err
		}
	}
	delete(persistedSubClient.SubStore.Store, sub.ID)

	newBytes, err := json.MarshalIndent(&persistedSubClient, "", " ")
	if err != nil {
		log.Errorf("error deleting sub %v", err)
		return err
	}
	return os.WriteFile(filePath, newBytes, 0666)
}

// loadFromFile is used to read subscriptionOne/publisher from the file system
func loadFromFile(filePath string) (b []byte, err error) {
	//open file
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	//read file and unmarshall json file to slice of users
	b, err = io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func loadFileNamesFromDir(filePath string) (subFiles []string, err error) {
	files, err := os.ReadDir(filePath)
	if err != nil {
		return subFiles, err
	}
	for _, file := range files {
		if !file.IsDir() {
			subFiles = append(subFiles, file.Name())
		}
	}
	return
}

// writeToFile writes subscriptionOne data to a file
func writeToFile(subscriberClient subscriber.Subscriber, filePath string) error {
	//open file
	mu.Lock()
	defer mu.Unlock()
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	//read file and unmarshall json file to slice of users
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	var persistedSubClient subscriber.Subscriber
	if len(b) > 0 {
		err = json.Unmarshal(b, &persistedSubClient)
		if err != nil {
			return err
		}
	} else {
		persistedSubClient = *subscriber.New(subscriberClient.ClientID)
	} // no  file found
	_ = persistedSubClient.SetEndPointURI(subscriberClient.GetEndPointURI())
	persistedSubClient.SetStatus(subscriber.Active)
	for subID, sub := range subscriberClient.SubStore.Store {
		persistedSubClient.SubStore.Store[subID] = sub
	}

	newBytes, err := json.MarshalIndent(&persistedSubClient, "", " ")
	if err != nil {
		return err
	}
	log.Infof("persisting following contents %s to a file %s\n", string(newBytes), filePath)
	return os.WriteFile(filePath, newBytes, 0666)
}

func hasDir(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.Mkdir(path, 0700)
	}
}
