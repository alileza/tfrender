resource "aws_instance" "example" {
  instance_type = var.instance_type
  hello         = true
  ami           = var.ami
  whatever      = var.whatever
  somevar       = "hehe"
  examplebool   = var.examplebool
  examplenum    = var.examplenum
  examplefloat  = var.examplefloat
  count         = var.instance_count
}
