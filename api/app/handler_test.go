// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"github.com/globocom/commandmocker"
	"github.com/globocom/config"
	"github.com/globocom/tsuru/api/auth"
	"github.com/globocom/tsuru/api/bind"
	"github.com/globocom/tsuru/api/service"
	"github.com/globocom/tsuru/db"
	"github.com/globocom/tsuru/errors"
	"github.com/globocom/tsuru/log"
	"github.com/globocom/tsuru/repository"
	"io"
	"io/ioutil"
	"labix.org/v2/mgo/bson"
	. "launchpad.net/gocheck"
	stdlog "log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"time"
)

var output = `2012-06-05 17:03:36,887 WARNING ssl-hostname-verification is disabled for this environment
2012-06-05 17:03:36,887 WARNING EC2 API calls not using secure transport
2012-06-05 17:03:36,887 WARNING S3 API calls not using secure transport
2012-06-05 17:03:36,887 WARNING Ubuntu Cloud Image lookups encrypted but not authenticated
2012-06-05 17:03:36,896 INFO Connecting to environment...
2012-06-05 17:03:37,599 INFO Connected to environment.
2012-06-05 17:03:37,727 INFO Connecting to machine 0 at 10.170.0.191
export DATABASE_HOST=localhost
export DATABASE_USER=root
export DATABASE_PASSWORD=secret`

type testHandler struct {
	body    [][]byte
	method  []string
	url     []string
	content string
	header  []http.Header
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.method = append(h.method, r.Method)
	h.url = append(h.url, r.URL.String())
	b, _ := ioutil.ReadAll(r.Body)
	h.body = append(h.body, b)
	h.header = append(h.header, r.Header)
	w.Write([]byte(h.content))
}

type testBadHandler struct {
	body    [][]byte
	method  []string
	url     []string
	content string
	header  []http.Header
}

func (h *testBadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := stderrors.New("some error")
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *S) TestAppIsAvaliableHandlerShouldReturnsErrorWhenAppUnitStatusIsnotStarted(c *C) {
	a := App{
		Name:      "someapp",
		Framework: "python",
		Teams:     []string{s.team.Name},
		Units: []Unit{
			{
				Name:              "someapp/0",
				Type:              "django",
				AgentState:        "creating",
				MachineAgentState: "running",
				InstanceState:     "running",
			},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/avaliable?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppIsAvaliableHandler(recorder, request)
	c.Assert(err, NotNil)
}

func (s *S) TestAppIsAvaliableHandlerShouldReturns200WhenAppUnitStatusIsStarted(c *C) {
	a := App{
		Name:      "someapp",
		Framework: "python",
		Teams:     []string{s.team.Name},
		Units: []Unit{
			{
				Name:              "someapp/0",
				Type:              "django",
				AgentState:        "started",
				MachineAgentState: "running",
				InstanceState:     "running",
			},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/repository/clone?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppIsAvaliableHandler(recorder, request)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
}

func (s *S) TestCloneRepositoryHandlerShouldAddLogs(c *C) {
	output := `pre-restart:
  - pre.sh
pos-restart:
  - pos.sh
`
	dir, err := commandmocker.Add("juju", output)
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	a := App{
		Name:      "someapp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		Units: []Unit{
			{
				Name:              "someapp/0",
				Type:              "django",
				AgentState:        "started",
				MachineAgentState: "running",
				InstanceState:     "running",
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/repository/clone?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = CloneRepositoryHandler(recorder, request)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	messages := []string{
		" ---> Tsuru receiving push",
		" ---> Cloning your code in your machines",
		" ---> Installing dependencies",
		" ---> Deploy done!",
	}
	for _, msg := range messages {
		length, err := db.Session.Apps().Find(bson.M{"logs.message": msg}).Count()
		c.Check(err, IsNil)
		c.Check(length, Equals, 1)
	}
}

func (s *S) TestCloneRepositoryHandler(c *C) {
	output := `pre-restart:
  - pre.sh
pos-restart:
  - pos.sh
`
	dir, err := commandmocker.Add("juju", output)
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	u := Unit{
		Name:              "someapp/0",
		Type:              "django",
		AgentState:        "started",
		MachineAgentState: "running",
		InstanceState:     "running",
	}
	a := App{
		Name:      "someapp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		Units:     []Unit{u},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/repository/clone?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = CloneRepositoryHandler(recorder, request)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	regexp := `^# ---> Tsuru receiving push#.*
# ---> Cloning your code in your machines#.*
# ---> Installing dependencies#.*
# ---> Running pre-restart#.*
# ---> Restarting your app#.*
# ---> Running pos-restart#.*
# ---> Deploy done!##$
`
	c.Assert(strings.Replace(recorder.Body.String(), "\n", "#", -1), Matches, strings.Replace(regexp, "\n", "", -1))
	c.Assert(recorder.Header().Get("Content-Type"), Equals, "text")
}

func (s *S) TestCloneRepositoryRunsCloneOrPullThenPreRestartThenRestartThenPosRestartHooksInOrder(c *C) {
	w := new(bytes.Buffer)
	l := stdlog.New(w, "", stdlog.LstdFlags)
	log.SetLogger(l)
	output := `pre-restart:
  - pre.sh
pos-restart:
  - pos.sh
`
	dir, err := commandmocker.Add("juju", output)
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	u := Unit{
		Name:              "someapp/0",
		Type:              "django",
		AgentState:        "started",
		MachineAgentState: "running",
		InstanceState:     "running",
	}
	a := App{
		Name:      "someapp",
		Framework: "django",
		Teams:     []string{s.team.Name},
		Units:     []Unit{u},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/repository/clone?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = CloneRepositoryHandler(recorder, request)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	str := strings.Replace(w.String(), "\n", "", -1)
	c.Assert(str, Matches, ".*executing git clone.*executing hook dependencies.*executing hook to restart.*Executing pos-restart hook.*")
}

func (s *S) TestCloneRepositoryShouldReturnNotFoundWhenAppDoesNotExist(c *C) {
	request, err := http.NewRequest("GET", "/apps/abc/repository/clone?:name=abc", nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = CloneRepositoryHandler(recorder, request)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App abc not found.$")
}

func (s *S) TestAppList(c *C) {
	app1 := App{
		Name:  "app1",
		Teams: []string{s.team.Name},
		Units: []Unit{
			{Name: "app1/0", Ip: "10.10.10.10"},
		},
	}
	err := db.Session.Apps().Insert(app1)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app1.Name})
	app2 := App{
		Name:  "app2",
		Teams: []string{s.team.Name},
		Units: []Unit{
			{Name: "app2/0"},
		},
	}
	err = db.Session.Apps().Insert(app2)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app2.Name})
	expected := []App{app1, app2}
	request, err := http.NewRequest("GET", "/apps/", nil)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = AppList(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	apps := []App{}
	err = json.Unmarshal(body, &apps)
	c.Assert(err, IsNil)
	c.Assert(len(apps), Equals, len(expected))
	for i, app := range apps {
		c.Assert(app.Name, DeepEquals, expected[i].Name)
		if app.Units[0].Ip != "" {
			c.Assert(app.Units[0].Ip, Equals, "10.10.10.10")
		}
	}
}

// Issue #52.
func (s *S) TestAppListShouldListAllAppsOfAllTeamsThatTheUserIsAMember(c *C) {
	u := auth.User{Email: "passing-by@angra.com"}
	team := auth.Team{Name: "angra", Users: []string{s.user.Email, u.Email}}
	err := db.Session.Teams().Insert(team)
	c.Assert(err, IsNil)
	defer db.Session.Teams().Remove(bson.M{"_id": team.Name})
	app1 := App{
		Name:  "app1",
		Teams: []string{s.team.Name, "angra"},
	}
	err = db.Session.Apps().Insert(app1)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app1.Name})
	request, err := http.NewRequest("GET", "/apps/", nil)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = AppList(recorder, request, &u)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	var apps []App
	err = json.Unmarshal(body, &apps)
	c.Assert(err, IsNil)
	c.Assert(apps[0].Name, Equals, app1.Name)
}

func (s *S) TestListShouldReturnStatusNoContentWhenAppListIsNil(c *C) {
	request, err := http.NewRequest("GET", "/apps/", nil)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = AppList(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusNoContent)
}

func (s *S) TestDelete(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	myApp := App{
		Name:      "MyAppToDelete",
		Framework: "django",
		Teams:     []string{s.team.Name},
		Units: []Unit{
			{Ip: "10.10.10.10", Machine: 1},
		},
	}
	err = createApp(&myApp)
	c.Assert(err, IsNil)
	defer myApp.destroy()
	request, err := http.NewRequest("DELETE", "/apps/"+myApp.Name+"?:name="+myApp.Name, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppDelete(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	c.Assert(h.url[0], Equals, "/repository/MyAppToDelete") // increment the index because of createApp action
	c.Assert(h.method[0], Equals, "DELETE")
	c.Assert(string(h.body[0]), Equals, "null")
}

func (s *S) TestDeleteShouldReturnForbiddenIfTheGivenUserDoesNotHaveAccesToTheApp(c *C) {
	myApp := App{
		Name:      "MyAppToDelete",
		Framework: "django",
	}
	err := db.Session.Apps().Insert(myApp)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": myApp.Name})
	request, err := http.NewRequest("DELETE", "/apps/"+myApp.Name+"?:name="+myApp.Name, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppDelete(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^User does not have access to this app$")
}

func (s *S) TestDeleteShouldReturnNotFoundIfTheAppDoesNotExist(c *C) {
	request, err := http.NewRequest("DELETE", "/apps/unkown?:name=unknown", nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppDelete(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestDeleteShouldHandleWithGandalfError(c *C) {
	h := testBadHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	myApp := App{
		Name:      "MyAppToDelete",
		Framework: "django",
		Teams:     []string{s.team.Name},
		Units: []Unit{
			{Ip: "10.10.10.10", Machine: 1},
		},
	}
	err = createApp(&myApp)
	c.Assert(err, IsNil)
	defer myApp.destroy()
	request, err := http.NewRequest("DELETE", "/apps/"+myApp.Name+"?:name="+myApp.Name, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppDelete(recorder, request, s.user)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "Could not remove app's repository at git server. Aborting...")
}

func (s *S) TestDeleteReturnsErrorIfAppDestroyFails(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	myApp := App{
		Name:      "MyAppToDelete",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(myApp)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": myApp.Name})
	request, err := http.NewRequest("DELETE", "/apps/"+myApp.Name+"?:name="+myApp.Name, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	dir, err := commandmocker.Error("juju", "$*", 1)
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	err = AppDelete(recorder, request, s.user)
	c.Assert(err, NotNil)
}

func (s *S) TestAppInfo(c *C) {
	expectedApp := App{
		Name:      "NewApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(expectedApp)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": expectedApp.Name})
	var myApp map[string]interface{}
	request, err := http.NewRequest("GET", "/apps/"+expectedApp.Name+"?:name="+expectedApp.Name, nil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c.Assert(err, IsNil)
	err = AppInfo(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	err = json.Unmarshal(body, &myApp)
	c.Assert(err, IsNil)
	c.Assert(myApp["Name"], Equals, expectedApp.Name)
	c.Assert(myApp["Repository"], Equals, repository.GetUrl(expectedApp.Name))
}

func (s *S) TestAppInfoReturnsForbiddenWhenTheUserDoesNotHaveAccessToTheApp(c *C) {
	expectedApp := App{
		Name:      "NewApp",
		Framework: "django",
	}
	err := db.Session.Apps().Insert(expectedApp)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(expectedApp)
	request, err := http.NewRequest("GET", "/apps/"+expectedApp.Name+"?:name="+expectedApp.Name, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppInfo(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^User does not have access to this app$")
}

func (s *S) TestAppInfoReturnsNotFoundWhenAppDoesNotExist(c *C) {
	myApp := App{Name: "SomeApp"}
	request, err := http.NewRequest("GET", "/apps/"+myApp.Name+"?:name="+myApp.Name, nil)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = AppInfo(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App SomeApp not found.$")
}

func (s *S) TestCreateAppHelperCreatesRepositoryInGandalf(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	dir, err := commandmocker.Add("juju", "")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	a := App{Name: "someapp"}
	_, err = createAppHelper(&a, s.user)
	c.Assert(err, IsNil)
	defer a.destroy()
	c.Assert(h.url[0], Equals, "/repository")
	c.Assert(h.method[0], Equals, "POST")
	expected := fmt.Sprintf(`{"name":"someapp","users":["%s"],"ispublic":false}`, s.user.Email)
	c.Assert(string(h.body[0]), Equals, expected)
}

func (s *S) TestCreateAppHelperGrantsTeamAccessInGandalf(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	dir, err := commandmocker.Add("juju", "")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	a := App{Name: "someapp"}
	_, err = createAppHelper(&a, s.user)
	c.Assert(err, IsNil)
	defer a.destroy()
	c.Assert(h.url[1], Equals, "/repository/grant")
	c.Assert(h.method[1], Equals, "POST")
	expected := fmt.Sprintf(`{"repositories":["someapp"],"users":["%s"]}`, s.user.Email)
	c.Assert(string(h.body[1]), Equals, expected)
}

func (s *S) TestCreateAppHandler(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	dir, err := commandmocker.Add("juju", "")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	a := App{Name: "someapp"}
	defer func() {
		err := a.Get()
		c.Assert(err, IsNil)
		err = a.destroy()
		c.Assert(err, IsNil)
	}()
	b := strings.NewReader(`{"name":"someapp", "framework":"django"}`)
	request, err := http.NewRequest("POST", "/apps", b)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = CreateAppHandler(recorder, request, s.user)
	c.Assert(err, IsNil)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	repoUrl := repository.GetUrl(a.Name)
	var obtained map[string]string
	expected := map[string]string{
		"status":         "success",
		"repository_url": repoUrl,
	}
	err = json.Unmarshal(body, &obtained)
	c.Assert(obtained, DeepEquals, expected)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	var gotApp App
	err = db.Session.Apps().Find(bson.M{"name": "someapp"}).One(&gotApp)
	c.Assert(err, IsNil)
	_, found := gotApp.find(&s.team)
	c.Assert(found, Equals, true)
}

func (s *S) TestCreateAppReturnsPreconditionFailedIfTheAppNameIsInvalid(c *C) {
	b := strings.NewReader(`{"name":"123myapp","framework":"django"}`)
	request, err := http.NewRequest("POST", "/apps", b)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = CreateAppHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusPreconditionFailed)
	msg := "Invalid app name, your app should have at most 63 " +
		"characters, containing only lower case letters, numbers, " +
		"underscores (_) or dashes (-), starting with letter or underscore."
	c.Assert(e.Error(), Equals, msg)
}

func (s *S) TestCreateAppReturns403IfTheUserIsNotMemberOfAnyTeam(c *C) {
	dir, err := commandmocker.Add("juju", "")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	u := &auth.User{Email: "thetrees@rush.com", Password: "123"}
	b := strings.NewReader(`{"name":"someapp", "framework":"django"}`)
	request, err := http.NewRequest("POST", "/apps", b)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = CreateAppHandler(recorder, request, u)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^In order to create an app, you should be member of at least one team$")
}

func (s *S) TestCreateAppReturnsConflictWithProperMessageWhenTheAppAlreadyExist(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:  "plainsofdawn",
		Units: []Unit{{Machine: 1}},
	}
	err = createApp(&a)
	c.Assert(err, IsNil)
	defer a.destroy()
	b := strings.NewReader(`{"name":"plainsofdawn", "framework":"django"}`)
	request, err := http.NewRequest("POST", "/apps", b)
	c.Assert(err, IsNil)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	err = CreateAppHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusConflict)
	c.Assert(e.Message, Equals, `There is already an app named "plainsofdawn".`)
}

func (s *S) TestAddTeamToTheApp(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	t := auth.Team{Name: "itshardteam", Users: []string{s.user.Email}}
	err := db.Session.Teams().Insert(t)
	c.Assert(err, IsNil)
	defer db.Session.Teams().RemoveAll(bson.M{"_id": t.Name})
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{t.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, s.team.Name, a.Name, s.team.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GrantAccessToTeamHandler(recorder, request, s.user)
	c.Assert(err, IsNil)
	err = a.Get()
	c.Assert(err, IsNil)
	_, found := a.find(&s.team)
	c.Assert(found, Equals, true)
}

func (s *S) TestGrantAccessToTeamReturn404IfTheAppDoesNotExist(c *C) {
	request, err := http.NewRequest("PUT", "/apps/a/b?:app=a&:team=b", nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GrantAccessToTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App a not found.$")
}

func (s *S) TestGrantAccessToTeamReturn403IfTheGivenUserDoesNotHasAccessToTheApp(c *C) {
	a := App{
		Name:      "itshard",
		Framework: "django",
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, s.team.Name, a.Name, s.team.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GrantAccessToTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^User does not have access to this app$")
}

func (s *S) TestGrantAccessToTeamReturn404IfTheTeamDoesNotExist(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/a?:app=%s&:team=a", a.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GrantAccessToTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^Team not found$")
}

func (s *S) TestGrantAccessToTeamReturn409IfTheTeamHasAlreadyAccessToTheApp(c *C) {
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, s.team.Name, a.Name, s.team.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GrantAccessToTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusConflict)
}

func (s *S) TestGrantAccessToTeamCallsGandalf(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	t := &auth.Team{Name: "anything", Users: []string{s.user.Email}}
	err := db.Session.Teams().Insert(t)
	c.Assert(err, IsNil)
	defer db.Session.Teams().Remove(bson.M{"_id": t.Name})
	a := App{
		Name:      "tsuru",
		Framework: "golang",
		Teams:     []string{t.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = grantAccessToTeam(a.Name, s.team.Name, s.user)
	c.Assert(err, IsNil)
	c.Assert(h.url[0], Equals, "/repository/grant")
	c.Assert(h.method[0], Equals, "POST")
	expected := fmt.Sprintf(`{"repositories":["%s"],"users":["%s"]}`, a.Name, s.user.Email)
	c.Assert(string(h.body[0]), Equals, expected)
}

func (s *S) TestRevokeAccessFromTeam(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	t := auth.Team{Name: "abcd"}
	err := db.Session.Teams().Insert(t)
	c.Assert(err, IsNil)
	defer db.Session.Teams().Remove(bson.M{"_id": t.Name})
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{"abcd", s.team.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, s.team.Name, a.Name, s.team.Name)
	request, err := http.NewRequest("DELETE", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RevokeAccessFromTeamHandler(recorder, request, s.user)
	c.Assert(err, IsNil)
	a.Get()
	_, found := a.find(&s.team)
	c.Assert(found, Equals, false)
}

func (s *S) TestRevokeAccessFromTeamReturn404IfTheAppDoesNotExist(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	request, err := http.NewRequest("DELETE", "/apps/a/b?:app=a&:team=b", nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RevokeAccessFromTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App a not found.$")
}

func (s *S) TestRevokeAccessFromTeamReturn401IfTheGivenUserDoesNotHavePermissionInTheApp(c *C) {
	a := App{
		Name:      "itshard",
		Framework: "django",
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, s.team.Name, a.Name, s.team.Name)
	request, err := http.NewRequest("DELETE", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RevokeAccessFromTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^User does not have access to this app$")
}

func (s *S) TestRevokeAccessFromTeamReturn404IfTheTeamDoesNotExist(c *C) {
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/x?:app=%s&:team=x", a.Name, a.Name)
	request, err := http.NewRequest("DELETE", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RevokeAccessFromTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^Team not found$")
}

func (s *S) TestRevokeAccessFromTeamReturn404IfTheTeamDoesNotHaveAccessToTheApp(c *C) {
	t := auth.Team{Name: "blaaa"}
	err := db.Session.Teams().Insert(t)
	c.Assert(err, IsNil)
	t2 := auth.Team{Name: "team2"}
	err = db.Session.Teams().Insert(t2)
	c.Assert(err, IsNil)
	defer db.Session.Teams().Remove(bson.M{"_id": bson.M{"$in": []string{"blaaa", "team2"}}})
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{s.team.Name, t2.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, t.Name, a.Name, t.Name)
	request, err := http.NewRequest("DELETE", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RevokeAccessFromTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
}

func (s *S) TestRevokeAccessFromTeamReturn403IfTheTeamIsTheLastWithAccessToTheApp(c *C) {
	a := App{
		Name:      "itshard",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/%s?:app=%s&:team=%s", a.Name, s.team.Name, a.Name, s.team.Name)
	request, err := http.NewRequest("DELETE", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RevokeAccessFromTeamHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^You can not revoke the access from this team, because it is the unique team with access to the app, and an app can not be orphaned$")
}

func (s *S) TestRevokeAccessFromTeamRemovesRepositoryFromGandalf(c *C) {
	h := testHandler{}
	ts := httptest.NewServer(&h)
	defer ts.Close()
	config.Set("git:server", ts.URL)
	t := auth.Team{Name: "anything", Users: []string{s.user.Email}}
	err := db.Session.Teams().Insert(t)
	c.Assert(err, IsNil)
	defer db.Session.Teams().Remove(bson.M{"_id": t.Name})
	a := App{
		Name:      "tsuru",
		Framework: "golang",
		Teams:     []string{t.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = grantAccessToTeam(a.Name, s.team.Name, s.user)
	c.Assert(err, IsNil)
	err = revokeAccessFromTeam(a.Name, s.team.Name, s.user)
	c.Assert(h.url[1], Equals, "/repository/revoke") //should inc the index (because of the grantAccess)
	c.Assert(h.method[1], Equals, "DELETE")
	expected := fmt.Sprintf(`{"repositories":["%s"],"users":["%s"]}`, a.Name, s.user.Email)
	c.Assert(string(h.body[1]), Equals, expected)
}

func (s *S) TestRunHandlerShouldExecuteTheGivenCommandInTheGivenApp(c *C) {
	dir, err := commandmocker.Add("juju", "$*")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	u := Unit{
		Name:              "someapp/0",
		Type:              "django",
		Machine:           10,
		AgentState:        "started",
		MachineAgentState: "running",
		InstanceState:     "running",
	}
	a := App{
		Name:      "secrets",
		Framework: "arch enemy",
		Teams:     []string{s.team.Name},
		Units:     []Unit{u},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/run/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("ls"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RunCommand(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Body.String(), Equals, "ssh -o StrictHostKeyChecking no -q 10 [ -f /home/application/apprc ] && source /home/application/apprc; [ -d /home/application/current ] && cd /home/application/current; ls")
	c.Assert(recorder.Header().Get("Content-Type"), Equals, "text")
}

func (s *S) TestRunHandlerReturnsTheOutputOfTheCommandEvenIfItFails(c *C) {
	dir, err := commandmocker.Add("juju", "$*")
	c.Assert(err, IsNil)
	u := Unit{
		Name:              "someapp/0",
		Type:              "django",
		Machine:           10,
		AgentState:        "started",
		MachineAgentState: "running",
		InstanceState:     "running",
	}
	a := App{
		Name:      "secrets",
		Framework: "arch enemy",
		Teams:     []string{s.team.Name},
		Units:     []Unit{u},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/run/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("ls"))
	c.Assert(err, IsNil)
	errdir, err := commandmocker.Error("juju", "juju failed", 1)
	c.Assert(err, IsNil)
	defer commandmocker.Remove(errdir)
	recorder := httptest.NewRecorder()
	err = RunCommand(recorder, request, s.user)
	c.Assert(err, NotNil)
	c.Assert(recorder.Body.String(), Equals, "juju failed")
}

func (s *S) TestRunHandlerReturnsBadRequestIfTheCommandIsMissing(c *C) {
	bodies := []io.Reader{nil, strings.NewReader("")}
	for _, body := range bodies {
		request, err := http.NewRequest("POST", "/apps/unknown/run/?:name=unkown", body)
		c.Assert(err, IsNil)
		recorder := httptest.NewRecorder()
		err = RunCommand(recorder, request, s.user)
		c.Assert(err, NotNil)
		e, ok := err.(*errors.Http)
		c.Assert(ok, Equals, true)
		c.Assert(e.Code, Equals, http.StatusBadRequest)
		c.Assert(e, ErrorMatches, "^You must provide the command to run$")
	}
}

func (s *S) TestRunHandlerReturnsInternalErrorIfReadAllFails(c *C) {
	b := s.getTestData("bodyToBeClosed.txt")
	request, err := http.NewRequest("POST", "/users", b)
	c.Assert(err, IsNil)
	request.Body.Close()
	recorder := httptest.NewRecorder()
	err = RunCommand(recorder, request, s.user)
	c.Assert(err, NotNil)
}

func (s *S) TestRunHandlerReturnsNotFoundIfTheAppDoesNotExist(c *C) {
	request, err := http.NewRequest("POST", "/apps/unknown/run/?:name=unknown", strings.NewReader("ls"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RunCommand(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestRunHandlerReturnsForbiddenIfTheGivenUserDoesNotHaveAccessToTheApp(c *C) {
	a := App{
		Name:      "secrets",
		Framework: "arch enemy",
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/run/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("ls"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RunCommand(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
}

func (s *S) TestGetEnvHandlerGetsEnvironmentVariableFromApp(c *C) {
	a := App{
		Name:      "everything-i-want",
		Framework: "gotthard",
		Teams:     []string{s.team.Name},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST":     {Name: "DATABASE_HOST", Value: "localhost", Public: true},
			"DATABASE_USER":     {Name: "DATABASE_USER", Value: "root", Public: true},
			"DATABASE_PASSWORD": {Name: "DATABASE_PASSWORD", Value: "secret", Public: false},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, strings.NewReader("DATABASE_HOST"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Body.String(), Equals, "DATABASE_HOST=localhost\n")
}

func (s *S) TestGetEnvHandlerShouldAcceptMultipleVariables(c *C) {
	a := App{
		Name:  "four-sticks",
		Teams: []string{s.team.Name},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST":     {Name: "DATABASE_HOST", Value: "localhost", Public: true},
			"DATABASE_USER":     {Name: "DATABASE_USER", Value: "root", Public: true},
			"DATABASE_PASSWORD": {Name: "DATABASE_PASSWORD", Value: "secret", Public: false},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, strings.NewReader("DATABASE_HOST DATABASE_USER"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Body.String(), Equals, "DATABASE_HOST=localhost\nDATABASE_USER=root\n")
}

func (s *S) TestGetEnvHandlerReturnsAllVariablesIfEnvironmentVariablesAreMissingWithMaskOnPrivateVars(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "time",
		Framework: "pink-floyd",
		Teams:     []string{s.team.Name},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST":     {Name: "DATABASE_HOST", Value: "localhost", Public: true},
			"DATABASE_USER":     {Name: "DATABASE_USER", Value: "root", Public: true},
			"DATABASE_PASSWORD": {Name: "DATABASE_PASSWORD", Value: "secret", Public: false},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	expected := []string{"", "DATABASE_HOST=localhost", "DATABASE_PASSWORD=*** (private variable)", "DATABASE_USER=root"}
	bodies := []io.Reader{nil, strings.NewReader("")}
	for _, body := range bodies {
		request, err := http.NewRequest("GET", "/apps/time/env/?:name=time", body)
		c.Assert(err, IsNil)
		recorder := httptest.NewRecorder()
		err = GetEnv(recorder, request, s.user)
		c.Assert(err, IsNil)
		got := strings.Split(recorder.Body.String(), "\n")
		sort.Strings(got)
		c.Assert(got, DeepEquals, expected)
	}
}

func (s *S) TestGetEnvHandlerReturnsInternalErrorIfReadAllFails(c *C) {
	b := s.getTestData("bodyToBeClosed.txt")
	request, err := http.NewRequest("GET", "/apps/unkown/env/?:name=unknown", b)
	c.Assert(err, IsNil)
	request.Body.Close()
	recorder := httptest.NewRecorder()
	err = GetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
}

func (s *S) TestGetEnvHandlerReturnsNotFoundIfTheAppDoesNotExist(c *C) {
	request, err := http.NewRequest("GET", "/apps/unknown/env/?:name=unknown", strings.NewReader("DATABASE_HOST"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestGetEnvHandlerReturnsForbiddenIfTheGivenUserDoesNotHaveAccessToTheApp(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "lost",
		Framework: "vougan",
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, strings.NewReader("DATABASE_HOST"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = GetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
}

func (s *S) TestSetEnvRespectsThePublicOnlyFlagKeepPrivateVariablesWhenItsTrue(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:  "myapp",
		Units: []Unit{{Machine: 1}},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	envs := []bind.EnvVar{
		{
			Name:   "DATABASE_HOST",
			Value:  "remotehost",
			Public: false,
		},
		{
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	err = setEnvsToApp(&a, envs, true)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "localhost",
			Public: false,
		},
		"DATABASE_PASSWORD": {
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	c.Assert(newApp.Env, DeepEquals, expected)
}

func (s *S) TestSetEnvRespectsThePublicOnlyFlagOverwrittenAllVariablesWhenItsFalse(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name: "myapp",
		Units: []Unit{
			{Machine: 1},
		},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	envs := []bind.EnvVar{
		{
			Name:   "DATABASE_HOST",
			Value:  "remotehost",
			Public: true,
		},
		{
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	err = setEnvsToApp(&a, envs, false)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "remotehost",
			Public: true,
		},
		"DATABASE_PASSWORD": {
			Name:   "DATABASE_PASSWORD",
			Value:  "123",
			Public: true,
		},
	}
	c.Assert(newApp.Env, DeepEquals, expected)
}

func (s *S) TestSetEnvHandlerShouldSetAPublicEnvironmentVariableInTheApp(c *C) {
	a := App{
		Name:  "black-dog",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=localhost"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "black-dog"}
	err = app.Get()
	c.Assert(err, IsNil)
	expected := bind.EnvVar{Name: "DATABASE_HOST", Value: "localhost", Public: true}
	c.Assert(app.Env["DATABASE_HOST"], DeepEquals, expected)
}

func (s *S) TestSetEnvHandlerShouldSetMultipleEnvironmentVariablesInTheApp(c *C) {
	a := App{
		Name:  "vigil",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=localhost DATABASE_USER=root"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "vigil"}
	err = app.Get()
	c.Assert(err, IsNil)
	expectedHost := bind.EnvVar{Name: "DATABASE_HOST", Value: "localhost", Public: true}
	expectedUser := bind.EnvVar{Name: "DATABASE_USER", Value: "root", Public: true}
	c.Assert(app.Env["DATABASE_HOST"], DeepEquals, expectedHost)
	c.Assert(app.Env["DATABASE_USER"], DeepEquals, expectedUser)
}

func (s *S) TestSetEnvHandlerShouldSupportSpacesInTheEnvironmentVariableValue(c *C) {
	a := App{
		Name:  "loser",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=local host DATABASE_USER=root"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "loser"}
	err = app.Get()
	c.Assert(err, IsNil)
	expectedHost := bind.EnvVar{Name: "DATABASE_HOST", Value: "local host", Public: true}
	expectedUser := bind.EnvVar{Name: "DATABASE_USER", Value: "root", Public: true}
	c.Assert(app.Env["DATABASE_HOST"], DeepEquals, expectedHost)
	c.Assert(app.Env["DATABASE_USER"], DeepEquals, expectedUser)
}

func (s *S) TestSetEnvHandlerShouldSupportValuesWithDot(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:  "losers",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=http://foo.com:8080"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "losers"}
	err = app.Get()
	c.Assert(err, IsNil)
	expected := bind.EnvVar{Name: "DATABASE_HOST", Value: "http://foo.com:8080", Public: true}
	c.Assert(app.Env["DATABASE_HOST"], DeepEquals, expected)
}

func (s *S) TestSetEnvHandlerShouldSupportNumbersOnVariableName(c *C) {
	a := App{
		Name:  "blinded",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("EC2_HOST=http://foo.com:8080"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "blinded"}
	err = app.Get()
	c.Assert(err, IsNil)
	expected := bind.EnvVar{Name: "EC2_HOST", Value: "http://foo.com:8080", Public: true}
	c.Assert(app.Env["EC2_HOST"], DeepEquals, expected)
}

func (s *S) TestSetEnvHandlerShouldSupportLowerCasedVariableName(c *C) {
	a := App{
		Name:  "fragments",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("http_proxy=http://my_proxy.com:3128"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "fragments"}
	err = app.Get()
	c.Assert(err, IsNil)
	expected := bind.EnvVar{Name: "http_proxy", Value: "http://my_proxy.com:3128", Public: true}
	c.Assert(app.Env["http_proxy"], DeepEquals, expected)
}

func (s *S) TestSetEnvHandlerShouldNotChangeValueOfPrivateVariables(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	original := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "privatehost.com",
			Public: false,
		},
	}
	a := App{
		Name:  "losers",
		Teams: []string{s.team.Name},
		Env:   original,
		Units: []Unit{{Machine: 1}},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=http://foo.com:8080"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := &App{Name: "losers"}
	err = app.Get()
	c.Assert(err, IsNil)
	c.Assert(app.Env, DeepEquals, original)
}

func (s *S) TestSetEnvHandlerReturnsInternalErrorIfReadAllFails(c *C) {
	b := s.getTestData("bodyToBeClosed.txt")
	request, err := http.NewRequest("POST", "/apps/unkown/env/?:name=unknown", b)
	c.Assert(err, IsNil)
	request.Body.Close()
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
}

func (s *S) TestSetEnvHandlerReturnsBadRequestIfVariablesAreMissing(c *C) {
	bodies := []io.Reader{nil, strings.NewReader("")}
	for _, body := range bodies {
		request, err := http.NewRequest("POST", "/apps/unknown/env/?:name=unkown", body)
		c.Assert(err, IsNil)
		recorder := httptest.NewRecorder()
		err = SetEnv(recorder, request, s.user)
		c.Assert(err, NotNil)
		e, ok := err.(*errors.Http)
		c.Assert(ok, Equals, true)
		c.Assert(e.Code, Equals, http.StatusBadRequest)
		c.Assert(e, ErrorMatches, "^You must provide the environment variables$")
	}
}

func (s *S) TestSetEnvHandlerReturnsNotFoundIfTheAppDoesNotExist(c *C) {
	b := strings.NewReader("DATABASE_HOST=localhost")
	request, err := http.NewRequest("POST", "/apps/unknown/env/?:name=unknown", b)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestSetEnvHandlerReturnsForbiddenIfTheGivenUserDoesNotHaveAccessToTheApp(c *C) {
	a := App{Name: "rock-and-roll"}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=localhost"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = SetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
}

func (s *S) TestUnsetEnvHandlerRemovesTheEnvironmentVariablesFromTheApp(c *C) {
	a := App{
		Name:  "swift",
		Teams: []string{s.team.Name},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST":     {Name: "DATABASE_HOST", Value: "localhost", Public: true},
			"DATABASE_USER":     {Name: "DATABASE_USER", Value: "root", Public: true},
			"DATABASE_PASSWORD": {Name: "DATABASE_PASSWORD", Value: "secret", Public: false},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	expected := a.Env
	delete(expected, "DATABASE_HOST")
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("DELETE", url, strings.NewReader("DATABASE_HOST"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnsetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := App{Name: "swift"}
	err = app.Get()
	c.Assert(err, IsNil)
	c.Assert(app.Env, DeepEquals, expected)
}

func (s *S) TestUnsetEnvHandlerRemovesAllGivenEnvironmentVariables(c *C) {
	a := App{
		Name:  "let-it-be",
		Teams: []string{s.team.Name},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST":     {Name: "DATABASE_HOST", Value: "localhost", Public: true},
			"DATABASE_USER":     {Name: "DATABASE_USER", Value: "root", Public: true},
			"DATABASE_PASSWORD": {Name: "DATABASE_PASSWORD", Value: "secret", Public: false},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("DELETE", url, strings.NewReader("DATABASE_HOST DATABASE_USER"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnsetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := App{Name: "let-it-be"}
	err = app.Get()
	expected := map[string]bind.EnvVar{
		"DATABASE_PASSWORD": {
			Name:   "DATABASE_PASSWORD",
			Value:  "secret",
			Public: false,
		},
	}
	c.Assert(err, IsNil)
	c.Assert(app.Env, DeepEquals, expected)
}

func (s *S) TestUnsetHandlerDoesNotRemovePrivateVariables(c *C) {
	a := App{
		Name:  "letitbe",
		Teams: []string{s.team.Name},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST":     {Name: "DATABASE_HOST", Value: "localhost", Public: true},
			"DATABASE_USER":     {Name: "DATABASE_USER", Value: "root", Public: true},
			"DATABASE_PASSWORD": {Name: "DATABASE_PASSWORD", Value: "secret", Public: false},
		},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("DELETE", url, strings.NewReader("DATABASE_HOST DATABASE_USER DATABASE_PASSWORD"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnsetEnv(recorder, request, s.user)
	c.Assert(err, IsNil)
	app := App{Name: "letitbe"}
	err = app.Get()
	expected := map[string]bind.EnvVar{
		"DATABASE_PASSWORD": {
			Name:   "DATABASE_PASSWORD",
			Value:  "secret",
			Public: false,
		},
	}
	c.Assert(err, IsNil)
	c.Assert(app.Env, DeepEquals, expected)
}

func (s *S) TestUnsetEnvRespectsThePublicOnlyFlagKeepPrivateVariablesWhenItsTrue(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name: "myapp",
		Units: []Unit{
			{Machine: 1},
		},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
			"DATABASE_PASSWORD": {
				Name:   "DATABASE_PASSWORD",
				Value:  "123",
				Public: true,
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = unsetEnvFromApp(&a, []string{"DATABASE_HOST", "DATABASE_PASSWORD"}, true)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:   "DATABASE_HOST",
			Value:  "localhost",
			Public: false,
		},
	}
	c.Assert(newApp.Env, DeepEquals, expected)
}

func (s *S) TestUnsetEnvRespectsThePublicOnlyFlagUnsettingAllVariablesWhenItsFalse(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name: "myapp",
		Units: []Unit{
			{Machine: 1},
		},
		Env: map[string]bind.EnvVar{
			"DATABASE_HOST": {
				Name:   "DATABASE_HOST",
				Value:  "localhost",
				Public: false,
			},
			"DATABASE_PASSWORD": {
				Name:   "DATABASE_PASSWORD",
				Value:  "123",
				Public: true,
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	err = unsetEnvFromApp(&a, []string{"DATABASE_HOST", "DATABASE_PASSWORD"}, false)
	c.Assert(err, IsNil)
	newApp := App{Name: a.Name}
	err = newApp.Get()
	c.Assert(err, IsNil)
	c.Assert(newApp.Env, DeepEquals, map[string]bind.EnvVar{})
}

func (s *S) TestUnsetEnvHandlerReturnsInternalErrorIfReadAllFails(c *C) {
	b := s.getTestData("bodyToBeClosed.txt")
	request, err := http.NewRequest("POST", "/apps/unkown/env/?:name=unknown", b)
	c.Assert(err, IsNil)
	request.Body.Close()
	recorder := httptest.NewRecorder()
	err = UnsetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
}

func (s *S) TestUnsetEnvHandlerReturnsBadRequestIfVariablesAreMissing(c *C) {
	bodies := []io.Reader{nil, strings.NewReader("")}
	for _, body := range bodies {
		request, err := http.NewRequest("POST", "/apps/unknown/env/?:name=unkown", body)
		c.Assert(err, IsNil)
		recorder := httptest.NewRecorder()
		err = UnsetEnv(recorder, request, s.user)
		c.Assert(err, NotNil)
		e, ok := err.(*errors.Http)
		c.Assert(ok, Equals, true)
		c.Assert(e.Code, Equals, http.StatusBadRequest)
		c.Assert(e, ErrorMatches, "^You must provide the environment variables$")
	}
}

func (s *S) TestUnsetEnvHandlerReturnsNotFoundIfTheAppDoesNotExist(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	b := strings.NewReader("DATABASE_HOST")
	request, err := http.NewRequest("POST", "/apps/unknown/env/?:name=unknown", b)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnsetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestUnsetEnvHandlerReturnsForbiddenIfTheGivenUserDoesNotHaveAccessToTheApp(c *C) {
	a := App{Name: "mountain-mama"}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/env/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("POST", url, strings.NewReader("DATABASE_HOST=localhost"))
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnsetEnv(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
}

func (s *S) TestLogShouldReturnNotFoundWhenAppDoesNotExist(c *C) {
	request, err := http.NewRequest("GET", "/apps/unknown/log/?:name=unknown", nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppLog(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestLogReturnsForbiddenIfTheGivenUserDoesNotHaveAccessToTheApp(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "lost",
		Framework: "vougan",
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/log/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AppLog(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
}

func (s *S) TestAppLogShouldHaveContentType(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "lost",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
		Logs: []applog{
			{
				Date:    time.Now(),
				Message: "Something new",
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/log/?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	request.Header.Set("Content-Type", "application/json")
	err = AppLog(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Header().Get("Content-Type"), Equals, "application/json")
}

func (s *S) TestAppLogSelectByLines(c *C) {
	a := App{
		Name:      "lost",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	for i := 0; i < 15; i++ {
		a.log(strconv.Itoa(i), "source")
	}
	url := fmt.Sprintf("/apps/%s/log/?:name=%s&lines=10", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	request.Header.Set("Content-Type", "application/json")
	err = AppLog(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	logs := []applog{}
	err = json.Unmarshal(body, &logs)
	c.Assert(err, IsNil)
	c.Assert(logs, HasLen, 10)
}

func (s *S) TestAppLogSelectBySource(c *C) {
	a := App{
		Name:      "lost",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	a.log("mars log", "mars")
	a.log("earth log", "earth")
	url := fmt.Sprintf("/apps/%s/log/?:name=%s&source=mars", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	request.Header.Set("Content-Type", "application/json")
	err = AppLog(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	logs := []applog{}
	err = json.Unmarshal(body, &logs)
	c.Assert(err, IsNil)
	c.Assert(logs, HasLen, 1)
	c.Assert(logs[0].Message, Equals, "mars log")
	c.Assert(logs[0].Source, Equals, "mars")
}

func (s *S) TestAppLogSelectByLinesShouldReturnsTheLastestEntries(c *C) {
	a := App{
		Name:      "lost",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	logs := make([]applog, 15)
	now := time.Now()
	for i := 0; i < 15; i++ {
		logs[i] = applog{
			Date:    now.Add(time.Duration(i) * time.Hour),
			Message: strconv.Itoa(i),
			Source:  "source",
		}
	}
	a.Logs = logs
	err = db.Session.Apps().Update(bson.M{"name": a.Name}, a)
	c.Assert(err, IsNil)
	url := fmt.Sprintf("/apps/%s/log/?:name=%s&lines=3", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	request.Header.Set("Content-Type", "application/json")
	err = AppLog(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	logs = []applog{}
	err = json.Unmarshal(body, &logs)
	c.Assert(err, IsNil)
	c.Assert(logs, HasLen, 3)
	c.Assert(logs[0].Message, Equals, "12")
	c.Assert(logs[1].Message, Equals, "13")
	c.Assert(logs[2].Message, Equals, "14")
}

func (s *S) TestAppLogShouldReturnLogByApp(c *C) {
	app1 := App{
		Name:      "app1",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
	}
	err := db.Session.Apps().Insert(app1)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app1.Name})
	app1.log("app1 log", "source")
	app2 := App{
		Name:      "app2",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(app2)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app2.Name})
	app2.log("app2 log", "source")
	app3 := App{
		Name:      "app3",
		Framework: "vougan",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(app3)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": app3.Name})
	app3.log("app3 log", "tsuru")
	url := fmt.Sprintf("/apps/%s/log/?:name=%s", app3.Name, app3.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	request.Header.Set("Content-Type", "application/json")
	err = AppLog(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	body, err := ioutil.ReadAll(recorder.Body)
	c.Assert(err, IsNil)
	logs := []applog{}
	err = json.Unmarshal(body, &logs)
	c.Assert(err, IsNil)
	var logged bool
	for _, log := range logs {
		// Should not show the app1 log
		c.Assert(log.Message, Not(Equals), "app1 log")
		// Should not show the app2 log
		c.Assert(log.Message, Not(Equals), "app2 log")
		if log.Message == "app3 log" {
			logged = true
		}
	}
	// Should show the app3 log
	c.Assert(logged, Equals, true)
}

func (s *S) TestGetTeamNamesReturnTheNameOfTeamsThatTheUserIsMember(c *C) {
	one := &auth.User{Email: "imone@thewho.com", Password: "123"}
	who := auth.Team{Name: "TheWho", Users: []string{one.Email}}
	err := db.Session.Teams().Insert(who)
	what := auth.Team{Name: "TheWhat", Users: []string{one.Email}}
	err = db.Session.Teams().Insert(what)
	c.Assert(err, IsNil)
	where := auth.Team{Name: "TheWhere", Users: []string{one.Email}}
	err = db.Session.Teams().Insert(where)
	c.Assert(err, IsNil)
	teams := []string{who.Name, what.Name, where.Name}
	defer db.Session.Teams().RemoveAll(bson.M{"_id": bson.M{"$in": teams}})
	names, err := getTeamNames(one)
	c.Assert(err, IsNil)
	c.Assert(names, DeepEquals, teams)
}

func (s *S) TestBindHandler(c *C) {
	dir, err := commandmocker.Add("juju", "")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(dir)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"DATABASE_USER":"root","DATABASE_PASSWORD":"s3cr3t"}`))
	}))
	defer ts.Close()
	srvc := service.Service{Name: "mysql", Endpoint: map[string]string{"production": ts.URL}}
	err = srvc.Create()
	c.Assert(err, IsNil)
	defer db.Session.Services().Remove(bson.M{"_id": "mysql"})
	instance := service.ServiceInstance{
		Name:        "my-mysql",
		ServiceName: "mysql",
		Teams:       []string{s.team.Name},
	}
	err = instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	a := App{
		Name:  "painkiller",
		Teams: []string{s.team.Name},
		Units: []Unit{{Ip: "127.0.0.1", Machine: 1}},
		Env:   map[string]bind.EnvVar{},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/%s/%s?:instance=%s&:app=%s", instance.Name, a.Name, instance.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = BindHandler(recorder, request, s.user)
	c.Assert(err, IsNil)
	err = db.Session.ServiceInstances().Find(bson.M{"name": instance.Name}).One(&instance)
	c.Assert(err, IsNil)
	c.Assert(instance.Apps, DeepEquals, []string{a.Name})
	err = db.Session.Apps().Find(bson.M{"name": a.Name}).One(&a)
	c.Assert(err, IsNil)
	expectedUser := bind.EnvVar{Name: "DATABASE_USER", Value: "root", Public: false, InstanceName: instance.Name}
	expectedPassword := bind.EnvVar{Name: "DATABASE_PASSWORD", Value: "s3cr3t", Public: false, InstanceName: instance.Name}
	c.Assert(a.Env["DATABASE_USER"], DeepEquals, expectedUser)
	c.Assert(a.Env["DATABASE_PASSWORD"], DeepEquals, expectedPassword)
	var envs []string
	err = json.Unmarshal(recorder.Body.Bytes(), &envs)
	c.Assert(err, IsNil)
	sort.Strings(envs)
	c.Assert(envs, DeepEquals, []string{"DATABASE_PASSWORD", "DATABASE_USER"})
	c.Assert(recorder.Header().Get("Content-Type"), Equals, "application/json")
}

func (s *S) TestBindHandlerReturns404IfTheInstanceDoesNotExist(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "serviceApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/unknown/%s?:instance=unknown&:app=%s", a.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = BindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^Instance not found$")
}

func (s *S) TestBindHandlerReturns403IfTheUserDoesNotHaveAccessToTheInstance(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	instance := service.ServiceInstance{Name: "my-mysql", ServiceName: "mysql"}
	err = instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	a := App{
		Name:      "serviceApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/%s/%s?:instance=%s&:app=%s", instance.Name, a.Name, instance.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = BindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^This user does not have access to this instance$")
}

func (s *S) TestBindHandlerReturns404IfTheAppDoesNotExist(c *C) {
	instance := service.ServiceInstance{Name: "my-mysql", ServiceName: "mysql", Teams: []string{s.team.Name}}
	err := instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	url := fmt.Sprintf("/services/instances/%s/unknown?:instance=%s&:app=unknown", instance.Name, instance.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = BindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestBindHandlerReturns403IfTheUserDoesNotHaveAccessToTheApp(c *C) {
	instance := service.ServiceInstance{Name: "my-mysql", ServiceName: "mysql", Teams: []string{s.team.Name}}
	err := instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	a := App{
		Name:      "serviceApp",
		Framework: "django",
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/%s/%s?:instance=%s&:app=%s", instance.Name, a.Name, instance.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = BindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^This user does not have access to this app$")
}

func (s *S) TestUnbindHandler(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = r.Method == "DELETE" && r.URL.Path == "/resources/my-mysql/hostname/127.0.0.1"
	}))
	defer ts.Close()
	srvc := service.Service{Name: "mysql", Endpoint: map[string]string{"production": ts.URL}}
	err = srvc.Create()
	c.Assert(err, IsNil)
	defer db.Session.Services().Remove(bson.M{"_id": "mysql"})
	instance := service.ServiceInstance{
		Name:        "my-mysql",
		ServiceName: "mysql",
		Teams:       []string{s.team.Name},
		Apps:        []string{"painkiller"},
	}
	err = instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	a := App{
		Name:  "painkiller",
		Teams: []string{s.team.Name},
		Units: []Unit{{Machine: 1}},
	}
	err = createApp(&a)
	c.Assert(err, IsNil)
	defer a.destroy()
	a.Env = map[string]bind.EnvVar{
		"DATABASE_HOST": {
			Name:         "DATABASE_HOST",
			Value:        "arrea",
			Public:       false,
			InstanceName: instance.Name,
		},
		"MY_VAR": {
			Name:  "MY_VAR",
			Value: "123",
		},
	}
	a.Units = []Unit{{Ip: "127.0.0.1", Machine: 1}}
	err = db.Session.Apps().Update(bson.M{"name": a.Name}, &a)
	c.Assert(err, IsNil)
	url := fmt.Sprintf("/services/instances/%s/%s?:instance=%s&:app=%s", instance.Name, a.Name, instance.Name, a.Name)
	req, err := http.NewRequest("DELETE", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnbindHandler(recorder, req, s.user)
	c.Assert(err, IsNil)
	err = db.Session.ServiceInstances().Find(bson.M{"name": instance.Name}).One(&instance)
	c.Assert(err, IsNil)
	c.Assert(instance.Apps, DeepEquals, []string{})
	err = db.Session.Apps().Find(bson.M{"name": a.Name}).One(&a)
	c.Assert(err, IsNil)
	expected := map[string]bind.EnvVar{
		"MY_VAR": {
			Name:  "MY_VAR",
			Value: "123",
		},
	}
	c.Assert(a.Env, DeepEquals, expected)
	ch := make(chan bool)
	go func() {
		t := time.Tick(1)
		for _ = <-t; !called; _ = <-t {
		}
		ch <- true
	}()
	select {
	case <-ch:
		c.SucceedNow()
	case <-time.After(1e9):
		c.Errorf("Failed to call API after 1 second.")
	}
}

func (s *S) TestUnbindHandlerReturns404IfTheInstanceDoesNotExist(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:      "serviceApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/unknown/%s?:instance=unknown&:app=%s", a.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnbindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^Instance not found$")
}

func (s *S) TestUnbindHandlerReturns403IfTheUserDoesNotHaveAccessToTheInstance(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	instance := service.ServiceInstance{Name: "my-mysql", ServiceName: "mysql"}
	err = instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	a := App{
		Name:      "serviceApp",
		Framework: "django",
		Teams:     []string{s.team.Name},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/%s/%s?:instance=%s&:app=%s", instance.Name, a.Name, instance.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnbindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^This user does not have access to this instance$")
}

func (s *S) TestUnbindHandlerReturns404IfTheAppDoesNotExist(c *C) {
	instance := service.ServiceInstance{Name: "my-mysql", ServiceName: "mysql", Teams: []string{s.team.Name}}
	err := instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	url := fmt.Sprintf("/services/instances/%s/unknown?:instance=%s&:app=unknown", instance.Name, instance.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnbindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
	c.Assert(e, ErrorMatches, "^App unknown not found.$")
}

func (s *S) TestUnbindHandlerReturns403IfTheUserDoesNotHaveAccessToTheApp(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	instance := service.ServiceInstance{Name: "my-mysql", ServiceName: "mysql", Teams: []string{s.team.Name}}
	err = instance.Create()
	c.Assert(err, IsNil)
	defer db.Session.ServiceInstances().Remove(bson.M{"name": "my-mysql"})
	a := App{
		Name:      "serviceApp",
		Framework: "django",
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/services/instances/%s/%s?:instance=%s&:app=%s", instance.Name, a.Name, instance.Name, a.Name)
	request, err := http.NewRequest("PUT", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = UnbindHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
	c.Assert(e, ErrorMatches, "^This user does not have access to this app$")
}

func (s *S) TestRestartHandler(c *C) {
	tmpdir, err := commandmocker.Add("juju", "$*")
	c.Assert(err, IsNil)
	defer commandmocker.Remove(tmpdir)
	a := App{
		Name:  "stress",
		Teams: []string{s.team.Name},
		Units: []Unit{
			{
				AgentState:        "started",
				MachineAgentState: "running",
				InstanceState:     "running",
				Machine:           10,
				Ip:                "20.20.20.20",
			},
		},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/restart?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RestartHandler(recorder, request, s.user)
	c.Assert(err, IsNil)
	c.Assert(commandmocker.Ran(tmpdir), Equals, true)
	result := strings.Replace(recorder.Body.String(), "\n", "#", -1)
	c.Assert(result, Matches, ".*/var/lib/tsuru/hooks/restart.*")
	c.Assert(result, Matches, ".*# ---> Restarting your app#.*")
}

func (s *S) TestRestartHandlerReturns404IfTheAppDoesNotExist(c *C) {
	request, err := http.NewRequest("GET", "/apps/unknown/restart?:name=unknown", nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RestartHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusNotFound)
}

func (s *S) TestRestartHandlerReturns403IfTheUserDoesNotHaveAccessToTheApp(c *C) {
	a := App{Name: "nightmist"}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/restart?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RestartHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusForbidden)
}

func (s *S) TestRestartHandlerReturns412IfTheUnitOfTheAppDoesNotHaveIp(c *C) {
	dir, err := commandmocker.Add("juju", "")
	defer commandmocker.Remove(dir)
	c.Assert(err, IsNil)
	a := App{
		Name:  "stress",
		Teams: []string{s.team.Name},
		Units: []Unit{{Ip: "", Machine: 10}},
	}
	err = db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	url := fmt.Sprintf("/apps/%s/restart?:name=%s", a.Name, a.Name)
	request, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = RestartHandler(recorder, request, s.user)
	c.Assert(err, NotNil)
	e, ok := err.(*errors.Http)
	c.Assert(ok, Equals, true)
	c.Assert(e.Code, Equals, http.StatusPreconditionFailed)
	c.Assert(e.Message, Equals, "You can't restart this app because it doesn't have an IP yet.")
}

func (s *S) TestAddLogHandler(c *C) {
	a := App{
		Name:      "myapp",
		Framework: "python",
	}
	err := db.Session.Apps().Insert(a)
	c.Assert(err, IsNil)
	defer db.Session.Apps().Remove(bson.M{"name": a.Name})
	b := strings.NewReader(`["message 1", "message 2", "message 3"]`)
	request, err := http.NewRequest("POST", "/apps/myapp/log/?:name=myapp", b)
	c.Assert(err, IsNil)
	recorder := httptest.NewRecorder()
	err = AddLogHandler(recorder, request)
	c.Assert(err, IsNil)
	c.Assert(recorder.Code, Equals, http.StatusOK)
	messages := []string{
		"message 1",
		"message 2",
		"message 3",
	}
	for _, msg := range messages {
		length, err := db.Session.Apps().Find(bson.M{"name": a.Name, "logs.message": msg}).Count()
		c.Check(err, IsNil)
		c.Check(length, Equals, 1)
	}
}
