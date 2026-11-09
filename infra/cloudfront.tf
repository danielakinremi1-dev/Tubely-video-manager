# CloudFront serves media to browsers over HTTPS from edge caches. It's the
# only reader of the private bucket: Origin Access Control signs its origin
# requests (SigV4), and the bucket policy allows exactly this distribution.

resource "aws_cloudfront_origin_access_control" "media" {
  name                              = "tubely-media-${random_id.bucket_suffix.hex}"
  description                       = "Lets the Tubely distribution read the private media bucket"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# Object keys are random UUIDs and a re-upload always gets a new key, so an
# object never changes once written. That makes long edge caching safe, and
# no cache invalidation is ever needed.
data "aws_cloudfront_cache_policy" "caching_optimized" {
  name = "Managed-CachingOptimized"
}

resource "aws_cloudfront_distribution" "media" {
  enabled         = true
  comment         = "Tubely demo media"
  is_ipv6_enabled = true
  http_version    = "http2and3"

  # North America and Europe edges only: the cheapest price class.
  price_class = "PriceClass_100"

  origin {
    origin_id                = "media-bucket"
    domain_name              = aws_s3_bucket.media.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.media.id
  }

  default_cache_behavior {
    target_origin_id       = "media-bucket"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = data.aws_cloudfront_cache_policy.caching_optimized.id
    compress               = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  # The default *.cloudfront.net certificate: HTTPS with no domain or ACM
  # certificate to manage. A custom domain would need ACM in us-east-1.
  viewer_certificate {
    cloudfront_default_certificate = true
  }

  # Deliberately left out, to keep a demo at about $0: AWS WAF (billed
  # monthly per web ACL and rule) and access logging (needs a log bucket).
}
