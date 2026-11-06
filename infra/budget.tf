# A cost tripwire that exists whenever the stack does. It watches the whole
# account, not just Project=tubely: filtering by tag needs cost allocation
# tags activated first (up to 24 hours), and a tripwire should work from the
# first minute. Billing data itself lags by several hours, so this catches a
# forgotten stack, not a spike in progress.
resource "aws_budgets_budget" "demo" {
  name         = "tubely-demo-${random_id.bucket_suffix.hex}"
  budget_type  = "COST"
  limit_amount = "5"
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 1
    threshold_type             = "ABSOLUTE_VALUE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 5
    threshold_type             = "ABSOLUTE_VALUE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_email]
  }
}
