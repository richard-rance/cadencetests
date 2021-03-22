package internal

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/cadence"
	"go.uber.org/cadence/activity"
	"go.uber.org/cadence/client"
	"go.uber.org/cadence/workflow"
)

const CadenceDomain = "test-domain"

var CadenceClient client.Client

func init() {
	workflow.Register(Workflow)
	activity.Register(Activity)
}

// Use these query params to trigger different workflows from http://localhost:8090/?
type Params struct {
	PanicWorkflow bool
	PanicActivity int64
	AsyncWorkflow bool
	LocalActivity bool
}

func Processor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		return
	}
	fmt.Printf("Start processor\n")

	query := r.URL.Query()

	params := Params{}
	params.AsyncWorkflow, _ = strconv.ParseBool(query.Get("asyncWorkflow"))
	params.PanicWorkflow, _ = strconv.ParseBool(query.Get("panicWorkflow"))
	params.PanicActivity, _ = strconv.ParseInt(query.Get("panicActivity"), 10, 64)
	params.LocalActivity, _ = strconv.ParseBool(query.Get("localActivity"))

	fmt.Fprintf(w, "<html><body>")

	fmt.Fprintf(w, "<p>Params: %+v</p>", params)

	workflowID, runID, result, err := RunWorkflow(r.Context(), CadenceClient, params)
	fmt.Fprintf(w, "<p>View details <a href='http://localhost:8088/domains/test-domain/workflows/%v/%v/history?format=grid' target='_blank'>%v</a></p>", workflowID, runID, workflowID)
	fmt.Fprintf(w, "<p>Result:%v Error: <pre>%v</pre></p>", result, err)
	fmt.Fprintf(w, "</body></html>")
}

func RunWorkflow(ctx context.Context, CadenceClient client.Client, params Params) (workflowID, runID, result string, err error) {

	options := client.StartWorkflowOptions{
		TaskList:                     CadenceDomain,
		ExecutionStartToCloseTimeout: time.Hour * 24,
		WorkflowIDReusePolicy:        client.WorkflowIDReusePolicyAllowDuplicate,
		RetryPolicy: &cadence.RetryPolicy{
			InitialInterval:          time.Second,
			BackoffCoefficient:       2.0,
			MaximumInterval:          1 * time.Minute,
			ExpirationInterval:       1 * time.Minute,
			MaximumAttempts:          0,
			NonRetriableErrorReasons: nil,
		},
	}

	if params.AsyncWorkflow {
		fmt.Printf("Attempting to schedule\n")
		var exe *workflow.Execution
		exe, err = CadenceClient.StartWorkflow(ctx, options, Workflow, params)
		if err != nil {
			fmt.Printf("Could not schedule workflow %v\n", err)
			return
		}
		workflowID = exe.ID
		runID = exe.RunID

	} else {
		fmt.Printf("Attempting to run\n")
		var exe client.WorkflowRun
		exe, err = CadenceClient.ExecuteWorkflow(ctx, options, Workflow, params)
		if err != nil {
			fmt.Printf("Could not run workflow %v\n", err)
			return
		}
		workflowID = exe.GetID()
		runID = exe.GetRunID()
		err = exe.Get(ctx, &result)
	}

	return
}

func Workflow(ctx workflow.Context, params Params) error {
	ao := workflow.ActivityOptions{
		ScheduleToStartTimeout: time.Minute,
		StartToCloseTimeout:    2 * time.Minute,
		RetryPolicy: &cadence.RetryPolicy{
			InitialInterval:          time.Second,
			BackoffCoefficient:       2.0,
			MaximumInterval:          1 * time.Minute,
			ExpirationInterval:       1 * time.Minute,
			MaximumAttempts:          0,
			NonRetriableErrorReasons: nil,
		},
		HeartbeatTimeout: 10 * time.Second,
	}
	lao := workflow.LocalActivityOptions{
		ScheduleToCloseTimeout: time.Minute,
		RetryPolicy: &cadence.RetryPolicy{
			InitialInterval:          time.Second,
			BackoffCoefficient:       2.0,
			MaximumInterval:          1 * time.Minute,
			ExpirationInterval:       1 * time.Minute,
			MaximumAttempts:          0,
			NonRetriableErrorReasons: nil,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)
	ctx = workflow.WithLocalActivityOptions(ctx, lao)

	fmt.Printf("In Workflow\n")

	// TODO I could not figure out how to determine if the workflow was retrying. The attempt number does not seem to go up
	//      Instead it creates a new workflow after each panic.
	//      For testing manually flip the bool
	if params.PanicWorkflow && true {
		fmt.Printf("In Workflow: About to panic %v\n", workflow.GetInfo(ctx).Attempt)
		panic(fmt.Errorf("randomly breaking to see if workflow is retried"))
	}

	if params.LocalActivity {
		if err := workflow.ExecuteLocalActivity(ctx, Activity, params.PanicActivity).Get(ctx, nil); err != nil {
			fmt.Printf("Local activity returned final error %v", err)
		}
	} else {
		if err := workflow.ExecuteActivity(ctx, Activity, params.PanicActivity).Get(ctx, nil); err != nil {
			fmt.Printf("Standard activity returned final error %v", err)
		}
	}
	return nil
}

func Activity(ctx context.Context, panicTimes int64) error {
	if activity.GetInfo(ctx).Attempt < int32(panicTimes) {
		fmt.Printf("In activity: about to panic\n")
		panic(fmt.Errorf("randomly breaking to see if local activities are retried"))
	}
	fmt.Printf("In activity: No panic\n")
	return nil
}