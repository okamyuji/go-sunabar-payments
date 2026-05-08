variable "log_group_name" {
  description = "CloudWatch ロググループ名。"
  type        = string
}

variable "retention_days" {
  description = "保持日数。"
  type        = number
  default     = 7
}
