<?php
require 'vendor/autoload.php';
$app = require_once 'bootstrap/app.php';
$kernel = $app->make(Illuminate\Contracts\Console\Kernel::class);
$kernel->bootstrap();

$routes = Route::getRoutes();
foreach ($routes as $route) {
    if (str_contains($route->uri(), 'live-chat')) {
        echo $route->methods()[0] . " | " . $route->uri() . " | " . $route->getActionName() . "\n";
    }
}
